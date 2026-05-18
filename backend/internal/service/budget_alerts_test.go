package service_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service"
)

// seedSpend inserts an outflow against the user's fixture account so a
// later Alerts() call sees it.
func seedSpend(t *testing.T, g *gorm.DB, userID, accountID, categoryID int64, date time.Time, amt decimal.Decimal) {
	t.Helper()
	cat := categoryID
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: accountID, CategoryID: &cat,
		Amount: amt, Currency: "USD",
		TransactionDate: date, Source: "manual",
	}).Error; err != nil {
		t.Fatalf("seed txn: %v", err)
	}
}

// seedAccount returns a throwaway checking account owned by userID.
func seedAccount(t *testing.T, g *gorm.DB, userID int64) int64 {
	t.Helper()
	suffix := time.Now().Format("150405.000000")
	acc := &model.Account{
		UserID: userID, Name: "AlertAcct-" + suffix, InstitutionSlug: "fixture",
		AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	return acc.ID
}

// TestBudgetService_Alerts_Thresholds: three budgets at 50% / 85% / 110%
// of limit; only the latter two surface (warning, over).
func TestBudgetService_Alerts_Thresholds(t *testing.T) {
	svc, userID, fixtureCat, g := newBudgetSvc(t)
	ctx := context.Background()
	acctID := seedAccount(t, g, userID)

	// We need three distinct categories so we can have three separate
	// budgets on the same period.
	suffix := time.Now().Format("150405.000000")
	cats := []*model.Category{
		{Name: "AlertCat-50-" + suffix, Slug: "alert-cat-50-" + suffix},
		{Name: "AlertCat-85-" + suffix, Slug: "alert-cat-85-" + suffix},
		{Name: "AlertCat-110-" + suffix, Slug: "alert-cat-110-" + suffix},
	}
	for _, c := range cats {
		if err := g.Create(c).Error; err != nil {
			t.Fatalf("seed cat: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, c := range cats {
			g.Unscoped().Delete(&model.Category{}, c.ID)
		}
	})
	_ = fixtureCat // not used here; we use the locally-seeded categories

	// Three budgets, all monthly, all $100.
	budgets := []*model.Budget{}
	for _, c := range cats {
		b, err := svc.Create(ctx, userID, service.CreateBudgetInput{
			CategoryID: c.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
		})
		if err != nil {
			t.Fatalf("create budget: %v", err)
		}
		budgets = append(budgets, b)
	}
	t.Cleanup(func() {
		for _, b := range budgets {
			_ = svc.SoftDelete(ctx, userID, b.ID)
		}
	})

	// Clock is fixed at 2026-05-15 → monthly window = [May 1, Jun 1).
	// Spending in each category: 50, 85, 110 (outflow → negative amount).
	seedSpend(t, g, userID, acctID, cats[0].ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-50))
	seedSpend(t, g, userID, acctID, cats[1].ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-85))
	seedSpend(t, g, userID, acctID, cats[2].ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-110))

	alerts, err := svc.Alerts(ctx, userID)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2 (50%% excluded, 85%% warning, 110%% over)", len(alerts))
	}
	// Sort by category id so the assertions are deterministic.
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].CategoryID < alerts[j].CategoryID })

	if alerts[0].CategoryID != cats[1].ID {
		t.Errorf("first alert cat = %d, want %d (85%% bucket)", alerts[0].CategoryID, cats[1].ID)
	}
	if alerts[0].Severity != service.AlertWarning {
		t.Errorf("first severity = %v, want warning", alerts[0].Severity)
	}
	if alerts[1].CategoryID != cats[2].ID {
		t.Errorf("second alert cat = %d, want %d (110%% bucket)", alerts[1].CategoryID, cats[2].ID)
	}
	if alerts[1].Severity != service.AlertOver {
		t.Errorf("second severity = %v, want over", alerts[1].Severity)
	}
	if alerts[1].Pct < 1.0 {
		t.Errorf("over-budget pct = %v, want ≥1.0", alerts[1].Pct)
	}
}

// TestBudgetService_Alerts_InactiveExcluded: an inactive budget is never
// surfaced even when spending blows past the limit.
func TestBudgetService_Alerts_InactiveExcluded(t *testing.T) {
	svc, userID, _, g := newBudgetSvc(t)
	ctx := context.Background()
	acctID := seedAccount(t, g, userID)

	suffix := time.Now().Format("150405.000000")
	cat := &model.Category{Name: "Inactive-" + suffix, Slug: "inactive-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	inactive := false
	b, err := svc.Create(ctx, userID, service.CreateBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
		IsActive: &inactive,
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, b.ID) })

	seedSpend(t, g, userID, acctID, cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-500))

	alerts, err := svc.Alerts(ctx, userID)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	for _, a := range alerts {
		if a.BudgetID == b.ID {
			t.Errorf("inactive budget %d surfaced in alerts: %+v", b.ID, a)
		}
	}
}

// TestBudgetService_Alerts_TenantIsolation: user B's spending never
// appears in user A's alerts.
func TestBudgetService_Alerts_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newBudgetSvc(t)
	ctx := context.Background()

	userB := seedTestUser(t, g)
	acctB := seedAccount(t, g, userB)

	suffix := time.Now().Format("150405.000000")
	cat := &model.Category{Name: "IsoAlert-" + suffix, Slug: "iso-alert-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	// User A has a budget; user B does all the spending — A's alerts must be empty.
	b, err := svc.Create(ctx, userA, service.CreateBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("create A budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userA, b.ID) })

	seedSpend(t, g, userB, acctB, cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-9999))

	alerts, err := svc.Alerts(ctx, userA)
	if err != nil {
		t.Fatalf("Alerts: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("user A got %d alerts, want 0 — user B's spend leaked", len(alerts))
	}
}

// TestBudgetService_Alerts_OnlyAtThreshold: a budget at exactly 79.99%
// is excluded; 80% is included; 100% is "over". This pins down the
// boundary conditions.
func TestBudgetService_Alerts_OnlyAtThreshold(t *testing.T) {
	svc, userID, _, g := newBudgetSvc(t)
	ctx := context.Background()
	acctID := seedAccount(t, g, userID)

	suffix := time.Now().Format("150405.000000")
	cat := &model.Category{Name: "EdgeCat-" + suffix, Slug: "edge-cat-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	b, err := svc.Create(ctx, userID, service.CreateBudgetInput{
		CategoryID: cat.ID, Period: "monthly", Amount: decimal.NewFromInt(100),
	})
	if err != nil {
		t.Fatalf("create budget: %v", err)
	}
	t.Cleanup(func() { _ = svc.SoftDelete(ctx, userID, b.ID) })

	// 79.99 → no alert
	tx := &model.Transaction{
		UserID: userID, AccountID: acctID, CategoryID: &cat.ID,
		Amount:          decimal.RequireFromString("-79.99"),
		Currency:        "USD",
		TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	alerts, err := svc.Alerts(ctx, userID)
	if err != nil {
		t.Fatalf("Alerts at 79.99: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("79.99%%: got %d alerts, want 0", len(alerts))
	}

	// Add 0.01 → exactly 80%, triggers warning
	if err := g.Create(&model.Transaction{
		UserID: userID, AccountID: acctID, CategoryID: &cat.ID,
		Amount: decimal.RequireFromString("-0.01"), Currency: "USD",
		TransactionDate: time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		Source:          "manual",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	alerts, err = svc.Alerts(ctx, userID)
	if err != nil {
		t.Fatalf("Alerts at 80: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Severity != service.AlertWarning {
		t.Errorf("80%%: alerts = %+v, want one warning", alerts)
	}
}
