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

// TestBudgetService_LimitHistory: raising a limit applies from the current
// period on. The previous month keeps its old limit and remaining, and the
// rollover carried into this month comes from the old limit; two edits in
// one month leave one history row (#580). Clock = 2026-05-15.
func TestBudgetService_LimitHistory(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)
	may := func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }
	april := func() time.Time { return time.Date(2026, 4, 3, 9, 0, 0, 0, time.UTC) }

	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "BudgetLimitHistory-" + suffix, InstitutionSlug: "fixture",
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
		{time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), "-100.25"},
		{time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC), "-29.75"}, // April: 130
		{time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), "-40"},
	} {
		tx := &model.Transaction{
			UserID: userID, AccountID: acc.ID, CategoryID: &cat,
			Amount: decimal.RequireFromString(s.amt), TransactionDate: s.date, Source: "manual",
		}
		if err := g.Create(tx).Error; err != nil {
			t.Fatalf("seed txn: %v", err)
		}
	}

	// Created in April at 200, with rollover.
	svc.WithNow(april)
	roll := true
	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.NewFromInt(200), Rollover: &roll,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, b.ID) })
	if err := g.Model(&model.Budget{}).Where("id = ?", b.ID).Update("created_at", april()).Error; err != nil {
		t.Fatalf("backdate budget: %v", err)
	}

	// Raised to 300 in May, then to 350 the same month.
	svc.WithNow(may)
	for _, amt := range []string{"300", "350"} {
		a := decimal.RequireFromString(amt)
		if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Amount: &a}); err != nil {
			t.Fatalf("update to %s: %v", amt, err)
		}
	}
	// Saving the same amount again doesn't add history.
	same := decimal.RequireFromString("350.000")
	if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Amount: &same}); err != nil {
		t.Fatalf("update same: %v", err)
	}

	var hist []model.BudgetAmount
	if err := g.Where("budget_id = ?", b.ID).Order("effective_from").Find(&hist).Error; err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 ||
		hist[0].EffectiveFrom.Format("2006-01-02") != "2026-04-01" || !hist[0].Amount.Equal(decimal.NewFromInt(200)) ||
		hist[1].EffectiveFrom.Format("2006-01-02") != "2026-05-01" || !hist[1].Amount.Equal(decimal.NewFromInt(350)) {
		t.Fatalf("history = %+v, want [2026-04-01:200 2026-05-01:350]", hist)
	}

	cases := []struct {
		name                                string
		on                                  time.Time
		wantLimit, wantCarry, wantRemaining string
	}{
		// April keeps its own limit: 200 - 130 = 70.
		{"previous month", time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), "200", "0", "70"},
		// May: the new limit plus April's real 70 carried, not 350-130.
		{"current month", may(), "350", "70", "380"},
		// Before the budget existed: the oldest limit known, no carry.
		{"before creation", time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), "200", "0", "200"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := svc.SpendOn(ctx, userID, b.ID, tc.on)
			if err != nil {
				t.Fatalf("SpendOn: %v", err)
			}
			if !v.Limit.Equal(decimal.RequireFromString(tc.wantLimit)) ||
				!v.Carryover.Equal(decimal.RequireFromString(tc.wantCarry)) ||
				!v.Remaining.Equal(decimal.RequireFromString(tc.wantRemaining)) {
				t.Errorf("limit/carry/remaining = %s/%s/%s, want %s/%s/%s",
					v.Limit, v.Carryover, v.Remaining, tc.wantLimit, tc.wantCarry, tc.wantRemaining)
			}
		})
	}

	t.Run("current spend matches SpendOn today", func(t *testing.T) {
		v, err := svc.Spend(ctx, userID, b.ID)
		if err != nil {
			t.Fatalf("Spend: %v", err)
		}
		if !v.Limit.Equal(decimal.NewFromInt(350)) || !v.Available.Equal(decimal.NewFromInt(420)) {
			t.Errorf("limit/available = %s/%s, want 350/420", v.Limit, v.Available)
		}
	})

	t.Run("another user's budget stays hidden", func(t *testing.T) {
		other := seedTestUser(t, g)
		if _, err := svc.SpendOn(ctx, other, b.ID, time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)); !errors.Is(err, service.ErrBudgetNotFound) {
			t.Errorf("other user err = %v, want ErrBudgetNotFound", err)
		}
	})
}

// TestBudgetService_LimitHistory_EditInCreationMonth: a budget created and
// raised in the same month still shows the original limit for earlier
// months; the edit applies from this month on (#580). Clock = 2026-05-15.
func TestBudgetService_LimitHistory_EditInCreationMonth(t *testing.T) {
	svc, userID, categoryID, g := newBudgetSvc(t)
	ctx := context.Background()
	owner := repository.UserOwner(userID)

	b, err := svc.Create(ctx, owner, service.CreateBudgetInput{
		CategoryID: categoryID, Period: "monthly", Amount: decimal.RequireFromString("200.10"),
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, owner, b.ID) })
	for _, amt := range []string{"300", "310.5"} {
		a := decimal.RequireFromString(amt)
		if _, err := svc.Update(ctx, owner, b.ID, service.UpdateBudgetInput{Amount: &a}); err != nil {
			t.Fatalf("update to %s: %v", amt, err)
		}
	}

	var hist []model.BudgetAmount
	if err := g.Where("budget_id = ?", b.ID).Order("effective_from").Find(&hist).Error; err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 ||
		hist[0].EffectiveFrom.Format("2006-01-02") != "2026-04-01" || !hist[0].Amount.Equal(decimal.RequireFromString("200.10")) ||
		hist[1].EffectiveFrom.Format("2006-01-02") != "2026-05-01" || !hist[1].Amount.Equal(decimal.RequireFromString("310.5")) {
		t.Fatalf("history = %+v, want [2026-04-01:200.10 2026-05-01:310.5]", hist)
	}
	for _, tc := range []struct {
		on   time.Time
		want string
	}{
		{time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC), "200.10"},
		{time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "200.10"},
		{time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), "310.5"},
	} {
		v, err := svc.SpendOn(ctx, userID, b.ID, tc.on)
		if err != nil {
			t.Fatalf("SpendOn %s: %v", tc.on.Format("2006-01-02"), err)
		}
		if !v.Limit.Equal(decimal.RequireFromString(tc.want)) {
			t.Errorf("limit on %s = %s, want %s", tc.on.Format("2006-01-02"), v.Limit, tc.want)
		}
	}
}
