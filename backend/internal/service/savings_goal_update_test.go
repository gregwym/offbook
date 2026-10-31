package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

// interleavingGoalRepo runs `before` once, just before delegating Update — the
// exact window #684 describes: after SavingsGoalService.Update has read its
// snapshot, before the repository writes it.
type interleavingGoalRepo struct {
	repository.SavingsGoalRepository
	before func()
}

func (r *interleavingGoalRepo) Update(ctx context.Context, g *model.SavingsGoal) error {
	if r.before != nil {
		f := r.before
		r.before = nil
		f()
	}
	return r.SavingsGoalRepository.Update(ctx, g)
}

// TestSavingsGoalService_Update_KeepsConcurrentContribution (#684): a
// metadata edit whose snapshot predates a committed contribution must not
// write the stale current_amount back. Covers personal and household goals.
func TestSavingsGoalService_Update_KeepsConcurrentContribution(t *testing.T) {
	_, userID, _, g := newGoalSvc(t)
	ctx := context.Background()

	hh := &model.Household{Name: "GoalRace-" + time.Now().Format("150405.000000")}
	if err := g.Create(hh).Error; err != nil {
		t.Fatalf("seed household: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("household_id = ?", hh.ID).Delete(&model.SavingsGoal{})
		g.Unscoped().Delete(&model.Household{}, hh.ID)
	})

	cases := []struct {
		name  string
		owner repository.PlanOwner
	}{
		{"personal", repository.UserOwner(userID)},
		{"household", repository.HouseholdOwner(hh.ID)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := &interleavingGoalRepo{SavingsGoalRepository: repository.NewSavingsGoalRepository(g)}
			svc := service.NewSavingsGoalService(wrapped, repository.NewAccountRepository(g))
			// A separate service on the plain repo plays "request B".
			other := service.NewSavingsGoalService(repository.NewSavingsGoalRepository(g), repository.NewAccountRepository(g))

			goal, err := svc.Create(ctx, tc.owner, service.CreateGoalInput{Name: "Before", TargetAmount: decimal.NewFromInt(1000)})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := other.Contribute(ctx, tc.owner, goal.ID, decimal.NewFromInt(100), userID); err != nil {
				t.Fatalf("seed 100: %v", err)
			}

			wrapped.before = func() {
				if _, err := other.Contribute(ctx, tc.owner, goal.ID, decimal.NewFromInt(50), userID); err != nil {
					t.Errorf("concurrent +50: %v", err)
				}
			}
			name := "Renamed"
			updated, err := svc.Update(ctx, tc.owner, goal.ID, service.UpdateGoalInput{Name: &name})
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			if updated.Name != "Renamed" {
				t.Errorf("returned name = %q", updated.Name)
			}
			if !updated.CurrentAmount.Equal(decimal.NewFromInt(150)) {
				t.Errorf("returned current_amount = %s, want 150", updated.CurrentAmount)
			}

			stored, err := other.Get(ctx, tc.owner, goal.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if stored.Name != "Renamed" || !stored.CurrentAmount.Equal(decimal.NewFromInt(150)) {
				t.Errorf("stored = (%q, %s), want (Renamed, 150)", stored.Name, stored.CurrentAmount)
			}
			hist, err := other.Contributions(ctx, tc.owner, goal.ID)
			if err != nil || len(hist) == 0 {
				t.Fatalf("history = %v, %v", hist, err)
			}
			if !hist[0].BalanceAfter.Equal(stored.CurrentAmount) {
				t.Errorf("latest history total %s disagrees with current_amount %s", hist[0].BalanceAfter, stored.CurrentAmount)
			}

			after, err := other.Contribute(ctx, tc.owner, goal.ID, decimal.NewFromInt(10), userID)
			if err != nil {
				t.Fatalf("+10: %v", err)
			}
			if !after.CurrentAmount.Equal(decimal.NewFromInt(160)) {
				t.Errorf("after +10 current_amount = %s, want 160", after.CurrentAmount)
			}
		})
	}
}

// TestSavingsGoalService_Update_MetadataSemantics: absent fields are left
// alone, explicit clears write NULL, and a deleted or foreign goal is not found.
func TestSavingsGoalService_Update_MetadataSemantics(t *testing.T) {
	svc, userID, accountID, g := newGoalSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	td := time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)
	goal, err := svc.Create(ctx, owner, service.CreateGoalInput{
		Name: "Trip", TargetAmount: decimal.RequireFromString("2500.000000000000000001"), TargetDate: &td, AccountID: &accountID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("sparse patch leaves other fields", func(t *testing.T) {
		amt := decimal.RequireFromString("3000.5")
		if _, err := svc.Update(ctx, owner, goal.ID, service.UpdateGoalInput{TargetAmount: &amt}); err != nil {
			t.Fatalf("update: %v", err)
		}
		got, _ := svc.Get(ctx, owner, goal.ID)
		if !got.TargetAmount.Equal(amt) || got.Name != "Trip" || got.TargetDate == nil || got.AccountID == nil || *got.AccountID != accountID {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("clears write NULL", func(t *testing.T) {
		if _, err := svc.Update(ctx, owner, goal.ID, service.UpdateGoalInput{ClearTargetDate: true, ClearAccountID: true}); err != nil {
			t.Fatalf("update: %v", err)
		}
		got, _ := svc.Get(ctx, owner, goal.ID)
		if got.TargetDate != nil || got.AccountID != nil {
			t.Errorf("target_date=%v account_id=%v, want both nil", got.TargetDate, got.AccountID)
		}
	})

	t.Run("other user cannot update", func(t *testing.T) {
		userB := seedTestUser(t, g)
		name := "Hijack"
		if _, err := svc.Update(ctx, repository.UserOwner(userB), goal.ID, service.UpdateGoalInput{Name: &name}); !errors.Is(err, service.ErrSavingsGoalNotFound) {
			t.Errorf("cross-tenant err = %v", err)
		}
	})

	t.Run("deleted goal is not found", func(t *testing.T) {
		if err := svc.SoftDelete(ctx, owner, goal.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		name := "Ghost"
		if _, err := svc.Update(ctx, owner, goal.ID, service.UpdateGoalInput{Name: &name}); !errors.Is(err, service.ErrSavingsGoalNotFound) {
			t.Errorf("deleted err = %v", err)
		}
	})
}
