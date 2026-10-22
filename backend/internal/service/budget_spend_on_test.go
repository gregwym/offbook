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

// TestBudgetService_SpendOn: ?on= picks the period containing the date, a
// past month's spent is that month's category spend, rollover carry is
// folded up to that month (0 before the budget existed), future dates are
// rejected and another user's budget stays hidden (#577). Clock = 2026-05-15.
func TestBudgetService_SpendOn(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "BudgetSpendOn-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	cat := categoryID
	for _, s := range []struct {
		date time.Time
		amt  string
	}{
		{time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), "-500"},
		{time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), "-40"},
		{time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), "-12.345"},
		{time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), "-8.005"},
		{time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC), "5"}, // refund nets April down
		{time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), "-30"},
	} {
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat,
			Amount: decimal.RequireFromString(s.amt), TransactionDate: s.date, Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}

	roll := true
	start := decimal.NewFromInt(10)
	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(100), Rollover: &roll, RolloverStart: &start,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, b.ID) })
	if err := g.Model(&model.Budget{}).Where("id = ?", b.ID).
		Update("created_at", time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)).Error; err != nil {
		t.Fatalf("backdate budget: %v", err)
	}

	cases := []struct {
		name                         string
		on                           time.Time
		wantStart                    time.Time
		wantSpent, wantCarry, wantRm string
	}{
		// Before creation: real spend, no carry (the start applies from March).
		{"before creation", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "500", "0", "-400"},
		// Creation month: the starting balance is the whole carry.
		{"creation month", time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), "40", "10", "70"},
		// April: carry 10+100-40=70; spent 12.345+8.005-5=15.35.
		{"past month", time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), "15.35", "70", "154.65"},
		// Today: same as Spend. May carry 70+100-15.35=154.65.
		{"today", time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), "30", "154.65", "224.65"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view, err := svc.SpendOn(ctx, userID, b.ID, tc.on)
			if err != nil {
				t.Fatalf("spend on: %v", err)
			}
			if !view.PeriodStart.Equal(tc.wantStart) || !view.PeriodEnd.Equal(tc.wantStart.AddDate(0, 1, 0)) {
				t.Errorf("window = %v → %v, want month of %v", view.PeriodStart, view.PeriodEnd, tc.wantStart)
			}
			if !view.Spent.Equal(decimal.RequireFromString(tc.wantSpent)) ||
				!view.Carryover.Equal(decimal.RequireFromString(tc.wantCarry)) ||
				!view.Remaining.Equal(decimal.RequireFromString(tc.wantRm)) {
				t.Errorf("spent/carry/remaining = %s/%s/%s, want %s/%s/%s",
					view.Spent, view.Carryover, view.Remaining, tc.wantSpent, tc.wantCarry, tc.wantRm)
			}
		})
	}

	t.Run("matches Spend for the current period", func(t *testing.T) {
		cur, err := svc.Spend(ctx, userID, b.ID)
		if err != nil {
			t.Fatalf("spend: %v", err)
		}
		on, err := svc.SpendOn(ctx, userID, b.ID, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("spend on: %v", err)
		}
		if !cur.Spent.Equal(on.Spent) || !cur.Available.Equal(on.Available) || !cur.PeriodStart.Equal(on.PeriodStart) {
			t.Errorf("Spend %+v != SpendOn %+v", cur, on)
		}
	})

	t.Run("future date rejected", func(t *testing.T) {
		_, err := svc.SpendOn(ctx, userID, b.ID, time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC))
		if !errors.Is(err, service.ErrBudgetDateInFuture) {
			t.Errorf("err = %v, want ErrBudgetDateInFuture", err)
		}
	})

	t.Run("other user's budget hidden", func(t *testing.T) {
		other := seedTestUser(t, g)
		_, err := svc.SpendOn(ctx, other, b.ID, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
		if !errors.Is(err, service.ErrBudgetNotFound) {
			t.Errorf("err = %v, want ErrBudgetNotFound", err)
		}
	})
}
