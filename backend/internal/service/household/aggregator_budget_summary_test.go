package household_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
)

// TestAggregator_BudgetSummary_PrivacyAndTotals (#589): the shared-budget
// month summary counts only balance_and_txns accounts of live members —
// private, balance_only and in-grace accounts never reach the category-level
// figures — and spent-in-budgets + not-in-a-budget equals the household
// Dashboard spending exactly.
func TestAggregator_BudgetSummary_PrivacyAndTotals(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()

	ownerID := seedUser(t, g, "bs-owner")
	partnerID := seedUser(t, g, "bs-partner")
	leaverID := seedUser(t, g, "bs-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "BS House", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	addMember(t, g, hh.ID, partnerID, model.RoleContributor, nil)
	leftAt := time.Now().Add(-7 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	shared := seedAccount(t, g, ownerID, "bs-shared")
	partnerShared := seedAccount(t, g, partnerID, "bs-partner-shared")
	balanceOnly := seedAccount(t, g, partnerID, "bs-balance-only")
	private := seedAccount(t, g, ownerID, "bs-private")
	leaver := seedAccount(t, g, leaverID, "bs-leaver")
	setShare(t, g, shared.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, partnerShared.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, balanceOnly.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, leaver.ID, hh.ID, model.VisibilityBalanceAndTxns)

	mkCat := func(name string) int64 {
		c := &model.Category{Name: name, Slug: fmt.Sprintf("bs-%s-%d", name, time.Now().UnixNano())}
		if err := g.Create(c).Error; err != nil {
			t.Fatalf("seed category: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, c.ID) })
		return c.ID
	}
	groceries, dining, paused := mkCat("groceries"), mkCat("dining"), mkCat("paused")
	hhID := hh.ID
	for _, b := range []*model.Budget{
		{HouseholdID: &hhID, CategoryID: groceries, Period: "monthly", Amount: decimal.NewFromInt(400), IsActive: true},
		{HouseholdID: &hhID, CategoryID: paused, Period: "monthly", Amount: decimal.NewFromInt(50), IsActive: false},
	} {
		if err := g.Create(b).Error; err != nil {
			t.Fatalf("seed shared budget: %v", err)
		}
		id := b.ID
		t.Cleanup(func() { g.Unscoped().Delete(&model.Budget{}, id) })
	}

	when := seedWhen()
	seedTxn(t, g, ownerID, shared.ID, "-120.50", &groceries, when)
	seedTxn(t, g, partnerID, partnerShared.ID, "-30.25", &groceries, when)
	seedTxn(t, g, ownerID, shared.ID, "-75.10", &dining, when)
	seedTxn(t, g, partnerID, partnerShared.ID, "-12.00", &paused, when) // paused budget: not in a budget
	seedTxn(t, g, ownerID, shared.ID, "-4.15", nil, when)
	seedTxn(t, g, partnerID, balanceOnly.ID, "-900", &dining, when) // balance_only: excluded
	seedTxn(t, g, ownerID, private.ID, "-800", &groceries, when)    // private: excluded
	seedTxn(t, g, leaverID, leaver.ID, "-700", &dining, when)       // in grace: excluded

	sum, err := agg.BudgetSummary(ctx, hh.ID)
	if err != nil {
		t.Fatalf("BudgetSummary: %v", err)
	}
	want := map[string]string{
		"budgeted": "400", "spent_in_budgets": "150.75", "remaining": "249.25",
		"not_in_budget": "91.25", "total_spending": "242",
	}
	got := map[string]string{
		"budgeted": sum.Budgeted, "spent_in_budgets": sum.SpentInBudgets, "remaining": sum.Remaining,
		"not_in_budget": sum.NotInBudget, "total_spending": sum.TotalSpending,
	}
	for k, w := range want {
		if !decimal.RequireFromString(got[k]).Equal(decimal.RequireFromString(w)) {
			t.Errorf("%s = %s, want %s (private, balance_only and in-grace rows must be excluded)", k, got[k], w)
		}
	}
	if sum.BudgetCount != 1 {
		t.Errorf("budget_count = %d, want 1 (paused budget excluded)", sum.BudgetCount)
	}
	breakdown := decimal.Zero
	for _, u := range sum.Unbudgeted {
		breakdown = breakdown.Add(decimal.RequireFromString(u.Spent))
	}
	if !breakdown.Equal(decimal.RequireFromString(sum.NotInBudget)) || len(sum.Unbudgeted) != 3 {
		t.Errorf("unbudgeted breakdown %s over %d rows, want %s over 3", breakdown, len(sum.Unbudgeted), sum.NotInBudget)
	}

	dash, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if !decimal.RequireFromString(dash.Spending).Equal(decimal.RequireFromString(sum.TotalSpending)) {
		t.Errorf("Dashboard spending %s != summary total %s", dash.Spending, sum.TotalSpending)
	}
}
