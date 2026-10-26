// Privacy and behavior coverage for the household month stepper (#619):
// SpendByCategory over a chosen month, and the month-anchored
// CategoryTrendThrough / CashFlowThrough. Each path must
//
//	(a) exclude private accounts
//	(b) exclude balance_only accounts from category-level figures
//	(c) exclude in-grace members from the live month
//
// (d) (no raw transaction rows) is the reflection walk in
// TestAggregator_NoRawTransactionRows, which covers CategorySpendingItem.
package household_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

func TestAggregator_MonthAnchored_PrivacyAndFigures(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	ownerID := seedUser(t, g, "month-owner")
	leaverID := seedUser(t, g, "month-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "MonthStep", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	full := seedAccount(t, g, ownerID, "month-full")
	balOnly := seedAccount(t, g, ownerID, "month-bal-only")
	priv := seedAccount(t, g, ownerID, "month-private")
	leaverAcct := seedAccount(t, g, leaverID, "month-leaver-acct")
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cat := &model.Category{Name: "MonthCat-" + suffix, Slug: "month-cat-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	apr := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	// April on the shared account: 3,000.00 in (income, not spending) and
	// 120.25 out. May: 40.00 out — must not appear in April's figures.
	seedTxn(t, g, ownerID, full.ID, "3000.00", nil, apr)
	seedTxn(t, g, ownerID, full.ID, "-120.25", &cat.ID, apr)
	seedTxn(t, g, ownerID, full.ID, "-40.00", &cat.ID, time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC))
	// None of these may surface in any figure.
	seedTxn(t, g, ownerID, balOnly.ID, "-9999", &cat.ID, apr)
	seedTxn(t, g, ownerID, priv.ID, "-9999", &cat.ID, apr)
	seedTxn(t, g, leaverID, leaverAcct.ID, "-9999", &cat.ID, apr)

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	t.Run("spend by category", func(t *testing.T) {
		rows, err := agg.SpendByCategory(ctx, hh.ID, from, to)
		if err != nil {
			t.Fatalf("SpendByCategory: %v", err)
		}
		if len(rows) != 1 || rows[0].CategoryID == nil || *rows[0].CategoryID != cat.ID || rows[0].Amount != "120.25" {
			t.Fatalf("rows = %+v, want one %s row of 120.25 (income excluded; balance_only/private/in-grace never counted)", rows, cat.Name)
		}
	})

	t.Run("spend by category defaults to the current month", func(t *testing.T) {
		rows, err := agg.SpendByCategory(ctx, hh.ID, time.Time{}, time.Time{})
		if err != nil {
			t.Fatalf("SpendByCategory: %v", err)
		}
		if len(rows) != 1 || rows[0].Amount != "40" {
			t.Fatalf("rows = %+v, want one May row of 40", rows)
		}
	})

	t.Run("cash flow through April", func(t *testing.T) {
		months, err := agg.CashFlowThrough(ctx, hh.ID, from, 1)
		if err != nil {
			t.Fatalf("CashFlowThrough: %v", err)
		}
		if len(months) != 1 {
			t.Fatalf("got %d months, want 1", len(months))
		}
		m := months[0]
		if m.Month != "2026-04-01" || m.Inflow != "3000" || m.Outflow != "120.25" || m.Net != "2879.75" {
			t.Errorf("April = %+v, want {2026-04-01 3000 120.25 2879.75}", m)
		}
	})

	t.Run("category trend through April", func(t *testing.T) {
		items, err := agg.CategoryTrendThrough(ctx, hh.ID, from, 2) // Mar, Apr
		if err != nil {
			t.Fatalf("CategoryTrendThrough: %v", err)
		}
		var got *string
		for _, it := range items {
			if it.CategoryID != nil && *it.CategoryID == cat.ID {
				got = &it.ThisMonth
				if it.TrailingAverage != "0" {
					t.Errorf("trailing average = %s, want 0 (nothing in March)", it.TrailingAverage)
				}
			}
		}
		if got == nil || *got != "120.25" {
			t.Errorf("items = %+v, want %s this_month 120.25 (April, not May's 40)", items, cat.Name)
		}
	})
}
