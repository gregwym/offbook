package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// seedRecurringTxn is a thin wrapper over seedMerchantTxn (dashboard_charts_test.go)
// for the common recurring-detection case: a flow, non-transfer outflow with
// just a merchant name.
func seedRecurringTxn(t *testing.T, g *gorm.DB, userID, accountID int64, merchant string, date time.Time, amt decimal.Decimal) {
	t.Helper()
	seedMerchantTxn(t, g, userID, accountID, "", &merchant, nil, nil, nil, date, amt, false)
}

func findRecurring(items []service.RecurringItem, merchant string) (service.RecurringItem, bool) {
	for _, it := range items {
		if it.Merchant == merchant {
			return it, true
		}
	}
	return service.RecurringItem{}, false
}

// TestDashboard_Recurring_MonthlyExact: same amount every month, five
// occurrences, gaps riding the calendar (28-31 days) — must classify as
// monthly with the right occurrence count and last amount.
func TestDashboard_Recurring_MonthlyExact(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	dates := []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC),
	}
	for _, d := range dates {
		seedRecurringTxn(t, g, userID, accountID, "NETFLIX", d, decimal.NewFromFloat(-15.99))
	}

	items, err := svc.Recurring(ctx, userID)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	item, ok := findRecurring(items, "NETFLIX")
	if !ok {
		t.Fatalf("NETFLIX not detected as recurring; items=%+v", items)
	}
	if item.Cadence != "monthly" {
		t.Errorf("cadence = %q, want monthly", item.Cadence)
	}
	if item.Occurrences != 5 {
		t.Errorf("occurrences = %d, want 5", item.Occurrences)
	}
	if item.LastAmount != "15.99" {
		t.Errorf("last_amount = %s, want 15.99", item.LastAmount)
	}
	if item.LastDate != "2026-05-05" {
		t.Errorf("last_date = %s, want 2026-05-05", item.LastDate)
	}
	next, err := time.Parse("2006-01-02", item.NextExpectedDate)
	if err != nil {
		t.Fatalf("next_expected_date not parseable: %v", err)
	}
	last, _ := time.Parse("2006-01-02", item.LastDate)
	gap := next.Sub(last).Hours() / 24
	if gap < 27 || gap > 33 {
		t.Errorf("next_expected_date gap = %.1f days, want a monthly gap (27-33)", gap)
	}
	me, err := decimal.NewFromString(item.MonthlyEquivalent)
	if err != nil {
		t.Fatalf("monthly_equivalent not a decimal: %v", err)
	}
	if me.Sub(decimal.RequireFromString("15.99")).Abs().GreaterThan(decimal.RequireFromString("1")) {
		t.Errorf("monthly_equivalent = %s, want ~15.99 for a monthly cadence", item.MonthlyEquivalent)
	}
}

// TestDashboard_Recurring_MonthlyDriftingAmountWithinTolerance: a utility
// bill that varies month to month but stays within the ±15% tolerance band
// must still be detected.
func TestDashboard_Recurring_MonthlyDriftingAmountWithinTolerance(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	amounts := []float64{-80, -85, -78, -90} // avg 83.25, max delta 6.75 well under 15% (~12.49)
	dates := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	for i, d := range dates {
		seedRecurringTxn(t, g, userID, accountID, "COMCAST", d, decimal.NewFromFloat(amounts[i]))
	}

	items, err := svc.Recurring(ctx, userID)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	item, ok := findRecurring(items, "COMCAST")
	if !ok {
		t.Fatalf("COMCAST not detected as recurring despite amount drift within tolerance; items=%+v", items)
	}
	if item.Cadence != "monthly" || item.Occurrences != 4 {
		t.Errorf("got cadence=%s occurrences=%d, want monthly/4", item.Cadence, item.Occurrences)
	}
	if item.LastAmount != "90" {
		t.Errorf("last_amount = %s, want 90 (most recent occurrence)", item.LastAmount)
	}
}

// TestDashboard_Recurring_Annual: an annual premium, three occurrences one
// year apart, must classify as annual.
func TestDashboard_Recurring_Annual(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	dates := []time.Time{
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	amounts := []float64{-1200, -1205, -1195}
	for i, d := range dates {
		seedRecurringTxn(t, g, userID, accountID, "GEICO", d, decimal.NewFromFloat(amounts[i]))
	}

	items, err := svc.Recurring(ctx, userID)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	item, ok := findRecurring(items, "GEICO")
	if !ok {
		t.Fatalf("GEICO not detected as recurring; items=%+v", items)
	}
	if item.Cadence != "annual" {
		t.Errorf("cadence = %q, want annual", item.Cadence)
	}
	if item.Occurrences != 3 {
		t.Errorf("occurrences = %d, want 3", item.Occurrences)
	}
	// Monthly-equivalent of an annual ~1200 charge should land near 100/mo.
	me, _ := decimal.NewFromString(item.MonthlyEquivalent)
	if me.LessThan(decimal.NewFromInt(90)) || me.GreaterThan(decimal.NewFromInt(110)) {
		t.Errorf("monthly_equivalent = %s, want ~100 (1200/12)", item.MonthlyEquivalent)
	}
}

// TestDashboard_Recurring_NearMissesExcluded: histories that must NOT be
// classified as recurring — too few occurrences, irregular cadence, and
// amounts that vary beyond tolerance. A control merchant with a genuine
// monthly pattern proves the detector isn't just returning nothing.
func TestDashboard_Recurring_NearMissesExcluded(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	// Control: a real recurring charge, so an empty result wouldn't
	// silently pass this test.
	for _, d := range []time.Time{
		time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	} {
		seedRecurringTxn(t, g, userID, accountID, "SPOTIFY", d, decimal.NewFromFloat(-9.99))
	}

	// Only two occurrences — below the minimum of three.
	seedRecurringTxn(t, g, userID, accountID, "TOOFEW", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-20))
	seedRecurringTxn(t, g, userID, accountID, "TOOFEW", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-20))

	// Irregular gaps — 10, 45, 12 days — fit no single cadence window.
	seedRecurringTxn(t, g, userID, accountID, "IRREGULAR", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-30))
	seedRecurringTxn(t, g, userID, accountID, "IRREGULAR", time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-30))
	seedRecurringTxn(t, g, userID, accountID, "IRREGULAR", time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-30))
	seedRecurringTxn(t, g, userID, accountID, "IRREGULAR", time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-30))

	// Regular monthly gaps but amounts wildly different — exceeds the ±15%
	// tolerance band.
	seedRecurringTxn(t, g, userID, accountID, "VARIABLE", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-10))
	seedRecurringTxn(t, g, userID, accountID, "VARIABLE", time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-50))
	seedRecurringTxn(t, g, userID, accountID, "VARIABLE", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), decimal.NewFromFloat(-100))

	// Non-flow kind (trade leg) with otherwise-perfect cadence and an
	// explicit merchant-shaped description must never count — filtered at
	// the SQL layer, same #351 regression class.
	tradeMerchant := "TRADEUSD"
	for _, d := range []time.Time{
		time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	} {
		seedMerchantTxn(t, g, userID, accountID, model.KindTradeLeg, &tradeMerchant, nil, nil, nil, d, decimal.NewFromInt(-500), false)
	}

	// A transfer with a recurring-looking shape must never count.
	for _, d := range []time.Time{
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC),
	} {
		seedMerchantTxn(t, g, userID, accountID, "", ptrStr("TRANSFERCO"), nil, nil, nil, d, decimal.NewFromInt(-100), true)
	}

	// An inflow with a recurring-looking shape (e.g. paycheck) must never
	// count — this detector is spend-only.
	for _, d := range []time.Time{
		time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC),
	} {
		seedRecurringTxn(t, g, userID, accountID, "PAYCHECK", d, decimal.NewFromFloat(2000))
	}

	items, err := svc.Recurring(ctx, userID)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	if _, ok := findRecurring(items, "SPOTIFY"); !ok {
		t.Fatalf("control merchant SPOTIFY should be detected as recurring; items=%+v", items)
	}
	for _, merchant := range []string{"TOOFEW", "IRREGULAR", "VARIABLE", "TRADEUSD", "TRANSFERCO", "PAYCHECK"} {
		if _, ok := findRecurring(items, merchant); ok {
			t.Errorf("merchant %q should NOT be detected as recurring", merchant)
		}
	}
}

// TestDashboard_Recurring_TenantIsolation: user B's recurring charge must
// never appear in user A's recurring list (new repository read path →
// multi-tenant test rule).
func TestDashboard_Recurring_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newDashboardSvc(t)
	ctx := context.Background()
	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "RecurB-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accB.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})
	for _, d := range []time.Time{
		time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
	} {
		seedRecurringTxn(t, g, userB, accB.ID, "SNEAKY-SUB", d, decimal.NewFromFloat(-9999))
	}

	items, err := svc.Recurring(ctx, userA)
	if err != nil {
		t.Fatalf("Recurring: %v", err)
	}
	if _, ok := findRecurring(items, "SNEAKY-SUB"); ok {
		t.Errorf("user A saw user B's recurring charge — tenant isolation broken")
	}
}
