package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func newDashboardSvc(t *testing.T) (svc *service.DashboardService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)
	acc := &model.Account{
		UserID: userID, Name: "DashAcct-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
		Balance: decimal.NewFromInt(0),
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	svc = service.NewDashboardService(repository.NewDashboardRepository(g))
	svc.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	return svc, userID, acc.ID, g
}

func seedChartTxn(t *testing.T, g *gorm.DB, userID, accountID int64, categoryID *int64, date time.Time, amt decimal.Decimal, isTransfer bool) {
	t.Helper()
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, CategoryID: categoryID,
		Amount: amt, Currency: "USD",
		TransactionDate: date, Source: "manual",
		IsTransfer: isTransfer,
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// TestDashboard_SpendByCategory_OutflowsOnlyAndTransfersExcluded.
func TestDashboard_SpendByCategory_OutflowsOnlyAndTransfersExcluded(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	suffix := time.Now().Format("150405.000000")
	groceries := &model.Category{Name: "ChartGroceries-" + suffix, Slug: "chart-g-" + suffix, Color: ptrStr("#84CC16")}
	dining := &model.Category{Name: "ChartDining-" + suffix, Slug: "chart-d-" + suffix, Color: ptrStr("#F59E0B")}
	for _, c := range []*model.Category{groceries, dining} {
		if err := g.Create(c).Error; err != nil {
			t.Fatalf("seed cat: %v", err)
		}
	}
	t.Cleanup(func() {
		g.Unscoped().Delete(&model.Category{}, groceries.ID)
		g.Unscoped().Delete(&model.Category{}, dining.ID)
	})

	d1 := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	seedChartTxn(t, g, userID, accountID, &groceries.ID, d1, decimal.NewFromInt(-50), false) // outflow → counts
	seedChartTxn(t, g, userID, accountID, &groceries.ID, d1, decimal.NewFromInt(-25), false) // outflow → counts
	seedChartTxn(t, g, userID, accountID, &groceries.ID, d1, decimal.NewFromInt(200), false) // inflow → excluded
	seedChartTxn(t, g, userID, accountID, &dining.ID, d1, decimal.NewFromInt(-30), false)    // outflow → counts
	seedChartTxn(t, g, userID, accountID, &dining.ID, d1, decimal.NewFromInt(-999), true)    // transfer → excluded

	items, err := svc.SpendByCategory(ctx, userID, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("SpendByCategory: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	// Ordered DESC by amount → groceries (75) first, dining (30) second.
	if items[0].Name != groceries.Name {
		t.Errorf("first item = %q, want %q (largest)", items[0].Name, groceries.Name)
	}
	if items[0].Amount != "75" {
		t.Errorf("groceries amount = %s, want 75", items[0].Amount)
	}
	if items[1].Amount != "30" {
		t.Errorf("dining amount = %s, want 30", items[1].Amount)
	}
	if items[0].Color != "#84CC16" {
		t.Errorf("groceries color = %s, want #84CC16", items[0].Color)
	}
}

// TestDashboard_SpendByCategory_TenantIsolation.
func TestDashboard_SpendByCategory_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newDashboardSvc(t)
	ctx := context.Background()

	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "B-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accB.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})
	cat := &model.Category{Name: "BCat-" + time.Now().Format("150405.000000"), Slug: "b-cat-" + time.Now().Format("150405.000000")}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed cat: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	seedChartTxn(t, g, userB, accB.ID, &cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-9999), false)

	items, err := svc.SpendByCategory(ctx, userA, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("SpendByCategory: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("user A got %d items, want 0 — user B's spend leaked", len(items))
	}
}

// TestDashboard_CashFlow_EmptyMonthsPaddedZeros: a quiet month still
// appears as a zero row so the chart doesn't drop the gap.
func TestDashboard_CashFlow_EmptyMonthsPaddedZeros(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	// 12 months trailing May 2026 → window starts June 2025.
	// Put one txn in May 2026, one in March 2026; April 2026 must come out
	// as a zero row.
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-50), false)
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-30), false)
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(500), false)
	// Transfer in May must NOT count.
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-9999), true)

	rows, err := svc.CashFlow(ctx, userID, 12)
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("got %d rows, want 12", len(rows))
	}
	// Find the rows by month.
	byMonth := map[string]service.CashFlowMonth{}
	for _, r := range rows {
		byMonth[r.Month] = r
	}
	may := byMonth["2026-05-01"]
	if may.Inflow != "500" || may.Outflow != "50" || may.Net != "450" {
		t.Errorf("May 2026 = %+v, want inflow=500, outflow=50, net=450 (transfer must be excluded)", may)
	}
	apr := byMonth["2026-04-01"]
	if apr.Inflow != "0" || apr.Outflow != "0" {
		t.Errorf("April 2026 should be zero-padded, got %+v", apr)
	}
	mar := byMonth["2026-03-01"]
	if mar.Outflow != "30" {
		t.Errorf("March 2026 outflow = %s, want 30", mar.Outflow)
	}
}

// TestDashboard_NetWorth_BackDerives: today's balance is $1000. Two
// outflows ($-100 each) happened on May 5 and May 10. The month-end
// rows for April and earlier should back-derive to $1200 (current +
// undone $200 of spending).
func TestDashboard_NetWorth_BackDerives(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()

	// Update the seeded account balance to 1000 — newDashboardSvc creates
	// it at 0.
	if err := g.Model(&model.Account{}).Where("id = ?", accountID).
		Update("balance", decimal.NewFromInt(1000)).Error; err != nil {
		t.Fatalf("set balance: %v", err)
	}
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-100), false)
	seedChartTxn(t, g, userID, accountID, nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC), decimal.NewFromInt(-100), false)

	rows, err := svc.NetWorth(ctx, userID, 3) // March, April, May
	if err != nil {
		t.Fatalf("NetWorth: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// rows[0] = March 2026 end (2026-03-31): subtract all transactions
	// after that → -200 worth of outflows → balance at end-March was 1200.
	// rows[1] = April 2026 end (2026-04-30): same, also 1200 (no April txns).
	// rows[2] = May 2026 end (2026-05-31): no txns after that → 1000.
	if rows[0].Total != "1200" {
		t.Errorf("March 2026 net worth = %s, want 1200", rows[0].Total)
	}
	if rows[1].Total != "1200" {
		t.Errorf("April 2026 net worth = %s, want 1200", rows[1].Total)
	}
	if rows[2].Total != "1000" {
		t.Errorf("May 2026 net worth = %s, want 1000", rows[2].Total)
	}
}

// TestDashboard_NetWorth_TenantIsolation: user B's balance and txns
// don't appear in user A's net-worth trend.
func TestDashboard_NetWorth_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newDashboardSvc(t)
	ctx := context.Background()
	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "B-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
		Balance: decimal.NewFromInt(9999),
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, accB.ID) })

	rows, err := svc.NetWorth(ctx, userA, 3)
	if err != nil {
		t.Fatalf("NetWorth: %v", err)
	}
	for _, r := range rows {
		if r.Total != "0" {
			t.Errorf("user A net worth row = %s, want 0 (user B's $9999 must not leak)", r.Total)
		}
	}
}

func ptrStr(s string) *string { return &s }
