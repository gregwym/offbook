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
	"github.com/gregwym/offbook/backend/internal/service/valuation"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

func newDashboardSvc(t *testing.T) (svc *service.DashboardService, userID, accountID int64, g *gorm.DB) {
	t.Helper()
	g = openTestDB(t)
	userID = seedTestUser(t, g)
	acc := &model.Account{
		UserID: userID, Name: "DashAcct-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", acc.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, acc.ID)
	})
	svc = service.NewDashboardService(
		repository.NewDashboardRepository(g),
		repository.NewTransactionRepository(g),
		repository.NewUserRepository(g),
		valuation.NewService(
			repository.NewPositionRepository(g),
			repository.NewPriceRepository(g),
			repository.NewAssetRepository(g),
			repository.NewAccountRepository(g),
		),
	)
	svc.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	return svc, userID, acc.ID, g
}

// seedNetWorthTxn seeds a ledger transaction carrying an explicit asset + kind,
// so the unified net-worth trend can fold quantity per asset over time.
func seedNetWorthTxn(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, kind string, date time.Time, amount string) {
	t.Helper()
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: assetID,
		Kind:            kind,
		Amount:          decimal.RequireFromString(amount),
		TransactionDate: date,
		Source:          "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed net-worth txn: %v", err)
	}
}

func insertNetWorthPrice(t *testing.T, g *gorm.DB, assetID, quoteAssetID int64, price string, asOf time.Time) {
	t.Helper()
	p := &model.Price{
		AssetID: assetID, QuoteAssetID: quoteAssetID,
		Price: decimal.RequireFromString(price), AsOf: asOf, Source: "test",
	}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed price: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Price{}, p.ID) })
}

func seedChartTxn(t *testing.T, g *gorm.DB, userID, accountID int64, categoryID *int64, date time.Time, amt decimal.Decimal, isTransfer bool) {
	t.Helper()
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, CategoryID: categoryID,
		Amount:          amt,
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
// TestDashboard_NetWorth_TradeStepsWithFlatPrices: with prices flat (USD cash,
// quote == asset), the trend steps only when the quantity fold changes. An
// opening balance plus a later inflow produce a step at the inflow's month.
func TestDashboard_NetWorth_TradeStepsWithFlatPrices(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	usdID := testutil.LookupUSDAssetID(t, g)

	// Opening balance of 1000 in February (before the window), then +500 in
	// April. Fold: Mar-end 1000, Apr-end 1500, May-end 1500.
	seedNetWorthTxn(t, g, userID, accountID, usdID, model.KindOpeningBalance, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "1000")
	seedNetWorthTxn(t, g, userID, accountID, usdID, model.KindFlow, time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), "500")

	rows, err := svc.NetWorth(ctx, userID, 3) // March, April, May
	if err != nil {
		t.Fatalf("NetWorth: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	want := []string{"1000", "1500", "1500"}
	for i, w := range want {
		if rows[i].Total != w {
			t.Errorf("row[%d] (%s) total = %s, want %s", i, rows[i].Date, rows[i].Total, w)
		}
		if !rows[i].Complete {
			t.Errorf("row[%d] expected complete (USD == primary, always priced)", i)
		}
	}
}

// TestDashboard_NetWorth_PriceMovesWithNoTrades: holding a constant quantity of
// a non-primary asset, the trend moves purely with that asset's price — and a
// month-end before any price exists is reported incomplete, not silently $0.
func TestDashboard_NetWorth_PriceMovesWithNoTrades(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	usdID := testutil.LookupUSDAssetID(t, g)
	eurID := testutil.LookupAssetID(t, g, "EUR", "fiat")

	// Hold 100 EUR from January on — quantity never changes.
	seedNetWorthTxn(t, g, userID, accountID, eurID, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "100")
	// No EUR price in March; price appears in April and rises in May.
	insertNetWorthPrice(t, g, eurID, usdID, "1.20", time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC))
	insertNetWorthPrice(t, g, eurID, usdID, "1.50", time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))

	rows, err := svc.NetWorth(ctx, userID, 3) // March, April, May
	if err != nil {
		t.Fatalf("NetWorth: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// March: no EUR price → unpriced → incomplete, partial total 0.
	if rows[0].Complete || rows[0].Total != "0" {
		t.Errorf("March row = {total:%s complete:%v}, want {0 false} (EUR unpriced)", rows[0].Total, rows[0].Complete)
	}
	// April: 100 × 1.20 = 120; May: 100 × 1.50 = 150 — moving with price.
	if rows[1].Total != "120" || !rows[1].Complete {
		t.Errorf("April row = {total:%s complete:%v}, want {120 true}", rows[1].Total, rows[1].Complete)
	}
	if rows[2].Total != "150" || !rows[2].Complete {
		t.Errorf("May row = {total:%s complete:%v}, want {150 true}", rows[2].Total, rows[2].Complete)
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
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B: %v", err)
	}
	t.Cleanup(func() {
		g.Unscoped().Where("account_id = ?", accB.ID).Delete(&model.Transaction{})
		g.Unscoped().Delete(&model.Account{}, accB.ID)
	})
	// User B has real money; the fold must stay user-scoped.
	usdID := testutil.LookupUSDAssetID(t, g)
	seedNetWorthTxn(t, g, userB, accB.ID, usdID, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "9999")

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

// seedAllocationPosition inserts a live position row (with cleanup) for
// allocation tests — allocation reads current positions, not the fold.
func seedAllocationPosition(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, quantity string) {
	t.Helper()
	p := &model.Position{
		UserID: userID, AccountID: accountID, AssetID: assetID,
		Quantity: decimal.RequireFromString(quantity),
	}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Position{}, p.ID) })
}

// TestDashboard_Allocation_BucketsByKindAndFlagsUnpriced: positions roll up
// by asset kind in the user's primary currency; a kind containing an unpriced
// asset is flagged incomplete instead of silently summing to a partial value
// (#341, #282 contract).
func TestDashboard_Allocation_BucketsByKindAndFlagsUnpriced(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	usdID := testutil.LookupUSDAssetID(t, g)
	eurID := testutil.LookupAssetID(t, g, "EUR", "fiat")
	btcID := testutil.LookupAssetID(t, g, "BTC", "crypto")

	// fiat: 1000 USD (same-asset, prices at 1) + 100 EUR at 1.20 → 1120.
	seedAllocationPosition(t, g, userID, accountID, usdID, "1000")
	seedAllocationPosition(t, g, userID, accountID, eurID, "100")
	insertNetWorthPrice(t, g, eurID, usdID, "1.20", time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC))
	// crypto: 0.5 BTC with NO price → bucket present but incomplete, value 0.
	seedAllocationPosition(t, g, userID, accountID, btcID, "0.5")

	rows, err := svc.Allocation(ctx, userID)
	if err != nil {
		t.Fatalf("Allocation: %v", err)
	}
	byKind := map[string]service.AssetClassAllocation{}
	for _, r := range rows {
		byKind[r.Kind] = r
	}
	fiat, ok := byKind["fiat"]
	if !ok {
		t.Fatal("no fiat bucket")
	}
	if fiat.Value != "1120" || !fiat.Complete {
		t.Errorf("fiat = {value:%s complete:%v}, want {1120 true}", fiat.Value, fiat.Complete)
	}
	crypto, ok := byKind["crypto"]
	if !ok {
		t.Fatal("no crypto bucket (unpriced asset must still surface its kind)")
	}
	if crypto.Complete {
		t.Error("crypto.Complete = true, want false (BTC has no price chain)")
	}
	if crypto.Value != "0" {
		t.Errorf("crypto.Value = %s, want 0 (unpriced excluded, not coerced)", crypto.Value)
	}
}

// TestDashboard_Allocation_TenantIsolation: user B's positions never appear
// in user A's allocation (new repository read path → multi-tenant test rule).
func TestDashboard_Allocation_TenantIsolation(t *testing.T) {
	svc, userA, _, g := newDashboardSvc(t)
	ctx := context.Background()
	userB := seedTestUser(t, g)
	accB := &model.Account{
		UserID: userB, Name: "AllocB-" + time.Now().Format("150405.000000"),
		InstitutionSlug: "fixture", AccountType: "checking", Currency: "USD",
	}
	if err := g.Create(accB).Error; err != nil {
		t.Fatalf("seed B account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, accB.ID) })
	usdID := testutil.LookupUSDAssetID(t, g)
	seedAllocationPosition(t, g, userB, accB.ID, usdID, "9999")

	rows, err := svc.Allocation(ctx, userA)
	if err != nil {
		t.Fatalf("Allocation: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("user A allocation has %d rows, want 0 (user B's positions must not leak)", len(rows))
	}
}

// TestDashboard_Summarize_NetWorthCompleteness: the headline net worth goes
// through the valuation derivation (#344) — a fresh-priced portfolio reports
// complete; adding an unpriced asset flips the flag and the unpriced value
// is excluded, never coerced to $0-and-counted-as-fine.
func TestDashboard_Summarize_NetWorthCompleteness(t *testing.T) {
	svc, userID, accountID, g := newDashboardSvc(t)
	ctx := context.Background()
	usdID := testutil.LookupUSDAssetID(t, g)

	seedAllocationPosition(t, g, userID, accountID, usdID, "1000")

	summary, err := svc.Summarize(ctx, userID, service.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary.NetWorth != "1000" || !summary.NetWorthComplete {
		t.Errorf("summary = {net_worth:%s complete:%v}, want {1000 true}", summary.NetWorth, summary.NetWorthComplete)
	}

	// An unpriced asset joins the book → headline flips to partial and the
	// priced portion still sums.
	btcID := testutil.LookupAssetID(t, g, "BTC", "crypto")
	seedAllocationPosition(t, g, userID, accountID, btcID, "0.5")

	summary, err = svc.Summarize(ctx, userID, service.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Summarize (with unpriced): %v", err)
	}
	if summary.NetWorth != "1000" {
		t.Errorf("net_worth = %s, want 1000 (unpriced BTC excluded from the sum)", summary.NetWorth)
	}
	if summary.NetWorthComplete {
		t.Error("net_worth_complete = true, want false (BTC has no price chain)")
	}
}
