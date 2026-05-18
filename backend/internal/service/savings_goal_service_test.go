package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func newGoalSvc(t *testing.T) (svc *service.SavingsGoalService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "GoalFixture-" + suffix, InstitutionSlug: "fixture",
		AccountType: "savings", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("user_id = ?", userID).Delete(&model.SavingsGoal{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})

	svc = service.NewSavingsGoalService(
		repository.NewSavingsGoalRepository(g),
		repository.NewAccountRepository(g),
	)
	return svc, userID, acc.ID, g
}

func TestSavingsGoalService_Create_Validation(t *testing.T) {
	svc, userID, accountID, _ := newGoalSvc(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		in      service.CreateGoalInput
		wantErr error
	}{
		{
			"valid",
			service.CreateGoalInput{Name: "Emergency", TargetAmount: decimal.NewFromInt(10000), AccountID: &accountID},
			nil,
		},
		{
			"empty name",
			service.CreateGoalInput{Name: "  ", TargetAmount: decimal.NewFromInt(100)},
			service.ErrEmptyGoalName,
		},
		{
			"zero target",
			service.CreateGoalInput{Name: "Zero", TargetAmount: decimal.Zero},
			service.ErrInvalidTargetAmount,
		},
		{
			"negative target",
			service.CreateGoalInput{Name: "Neg", TargetAmount: decimal.NewFromInt(-5)},
			service.ErrInvalidTargetAmount,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := svc.Create(ctx, userID, tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if g == nil || g.ID == 0 {
				t.Fatalf("want created goal, got %+v", g)
			}
			t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, g.ID) })
		})
	}
}

// TestSavingsGoalService_Create_RejectsCrossUserAccount: linking to an
// account owned by another user must be rejected.
func TestSavingsGoalService_Create_RejectsCrossUserAccount(t *testing.T) {
	svc, userA, _, g := newGoalSvc(t)
	ctx := context.Background()

	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "B's", InstitutionSlug: "fixture",
		AccountType: "savings", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B's account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, accB.ID) })

	_, err := svc.Create(ctx, userA, service.CreateGoalInput{
		Name: "Hack", TargetAmount: decimal.NewFromInt(100), AccountID: &accB.ID,
	})
	if !errors.Is(err, service.ErrGoalAccountMismatch) {
		t.Errorf("err = %v, want ErrGoalAccountMismatch", err)
	}
}

// TestSavingsGoalService_TenantIsolation: user B cannot Get/Update/Delete
// user A's goal, nor contribute to it.
func TestSavingsGoalService_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, userA, service.CreateGoalInput{
		Name: "TenantIso", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userA, goal.ID) })

	userB := seedTestUser(t, g)
	if _, err := svc.Get(ctx, userB, goal.ID); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Get err = %v", err)
	}
	if err := svc.SoftDelete(ctx, userB, goal.ID); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Delete err = %v", err)
	}
	if _, err := svc.Contribute(ctx, userB, goal.ID, decimal.NewFromInt(100)); !errors.Is(err, service.ErrSavingsGoalNotFound) {
		t.Errorf("cross-tenant Contribute err = %v", err)
	}
}

// TestSavingsGoalService_Contribute_AtomicAndPostFetched: a deposit then a
// withdrawal yields a clean net balance and the returned row reflects it.
func TestSavingsGoalService_Contribute_AtomicAndPostFetched(t *testing.T) {
	svc, userID, _, _ := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, userID, service.CreateGoalInput{
		Name: "Atomic", TargetAmount: decimal.NewFromInt(500),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, goal.ID) })

	after, err := svc.Contribute(ctx, userID, goal.ID, decimal.NewFromInt(200))
	if err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if !after.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Errorf("after deposit = %s, want 200", after.CurrentAmount)
	}
	after, err = svc.Contribute(ctx, userID, goal.ID, decimal.NewFromInt(-50))
	if err != nil {
		t.Fatalf("withdrawal: %v", err)
	}
	if !after.CurrentAmount.Equal(decimal.NewFromInt(150)) {
		t.Errorf("after withdrawal = %s, want 150", after.CurrentAmount)
	}
	if _, err := svc.Contribute(ctx, userID, goal.ID, decimal.Zero); !errors.Is(err, service.ErrZeroContribution) {
		t.Errorf("zero contribution err = %v", err)
	}
}

// TestSavingsGoalService_Contribute_ConcurrentSafe: 50 concurrent +1
// contributions must end at exactly 50. A naive read+write would race
// and leave the total below 50.
func TestSavingsGoalService_Contribute_ConcurrentSafe(t *testing.T) {
	svc, userID, _, _ := newGoalSvc(t)
	ctx := context.Background()

	goal, err := svc.Create(ctx, userID, service.CreateGoalInput{
		Name: "Concurrent", TargetAmount: decimal.NewFromInt(1000),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, goal.ID) })

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Contribute(ctx, userID, goal.ID, decimal.NewFromInt(1)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent contribute err: %v", err)
	}

	g, err := svc.Get(ctx, userID, goal.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !g.CurrentAmount.Equal(decimal.NewFromInt(n)) {
		t.Errorf("final = %s, want %d — concurrent contributions raced", g.CurrentAmount, n)
	}
}

// TestSavingsGoalService_View_ProgressAndRemaining covers the pure
// progress/remaining computation without DB round-trips.
func TestSavingsGoalService_View_ProgressAndRemaining(t *testing.T) {
	cases := []struct {
		name    string
		current string
		target  string
		wantPct float64
		wantRem string
	}{
		{"half", "50", "100", 0.5, "50"},
		{"capped at 1 when over", "150", "100", 1.0, "0"},
		{"zero current", "0", "100", 0.0, "100"},
		{"zero target", "10", "0", 0.0, "-10"}, // remaining < 0 → 0; pct = 0
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur, _ := decimal.NewFromString(tc.current)
			tgt, _ := decimal.NewFromString(tc.target)
			v := service.View(&model.SavingsGoal{CurrentAmount: cur, TargetAmount: tgt})
			if v.ProgressPct != tc.wantPct {
				t.Errorf("Pct = %v, want %v", v.ProgressPct, tc.wantPct)
			}
			// "zero target" → expected remaining clamped to 0
			wantRem := tc.wantRem
			if tc.name == "zero target" {
				wantRem = "0"
			}
			expectedRem, _ := decimal.NewFromString(wantRem)
			if !v.Remaining.Equal(expectedRem) {
				t.Errorf("Remaining = %s, want %s", v.Remaining, expectedRem)
			}
		})
	}
}
