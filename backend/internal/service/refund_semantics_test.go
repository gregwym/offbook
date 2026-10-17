package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// TestRefund_NetsAgainstCategoryNotIncome pins the #444 policy: a positive
// flow line in a spending category is a refund — it reduces that category's
// spending (and so the headline, cash flow and budget actual) and is NOT
// income. Uncategorized and Income-category inflows remain income. Raw
// amounts are untouched.
func TestRefund_NetsAgainstCategoryNotIncome(t *testing.T) {
	dash, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	cat := &model.Category{Name: "Refund" + suffix, Slug: "refund-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	day := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)                                               // newDashboardSvc pins the clock to May 2026
	seedChartTxn(t, g, userID, accountID, &cat.ID, day, decimal.RequireFromString("-100.00"), false) // purchase
	seedChartTxn(t, g, userID, accountID, &cat.ID, day, decimal.RequireFromString("30.00"), false)   // partial refund
	seedChartTxn(t, g, userID, accountID, nil, day, decimal.RequireFromString("3200.00"), false)     // salary (uncategorized)

	eq := func(what, got string, exp string) {
		t.Helper()
		v, err := decimal.NewFromString(got)
		if err != nil || !v.Equal(decimal.RequireFromString(exp)) {
			t.Errorf("%s = %q, want %s", what, got, exp)
		}
	}

	sum, err := dash.Summarize(ctx, userID, service.PeriodCurrentMonth)
	if err != nil {
		t.Fatal(err)
	}
	eq("spending", sum.Spending, "70.00")
	eq("income", sum.Income, "3200.00")

	items, err := dash.SpendByCategory(ctx, userID, day, day.AddDate(0, 1, 0))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.CategoryID != nil && *it.CategoryID == cat.ID {
			found = true
			eq("category spend "+fmt.Sprint(it.Name), it.Amount, "70.00")
		}
	}
	if !found {
		t.Fatal("category missing from breakdown")
	}

	months, err := dash.CashFlow(ctx, userID, 1)
	if err != nil {
		t.Fatal(err)
	}
	last := months[len(months)-1]
	eq("cash flow income", last.Inflow, "3200.00")
	eq("cash flow spending", last.Outflow, "70.00")
}
