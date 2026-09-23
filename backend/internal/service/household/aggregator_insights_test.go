// Privacy and behavior coverage for the three /h/insights/* methods added in
// M10a: Allocation, NetWorthTrend, AccountSummaries. The static "no PII"
// guard and the reflection walk over return types live in aggregator_test.go;
// here we cover the three additions against the rules-of-the-road:
//
//	(a) private accounts excluded from aggregates
//	(c) in-grace members excluded from live aggregates
//
// (b) (balance_only excluded from category breakdown), (d) (no raw txn
// rows in return types) and (e) (AI cross-member leak) don't apply to
// these methods or are covered by the existing reflection check in
// TestAggregator_NoRawTransactionRows.
package household_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/service/household"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// seedAsset creates a non-fiat asset with the given native quote, returning
// the new asset id. Used to construct an "equity" class entry for the
// allocation test without colliding with the seeded fiat rows.
func seedAsset(t *testing.T, g *gorm.DB, symbol, kind string, quoteAssetID int64) int64 {
	t.Helper()
	a := &model.Asset{Symbol: symbol, Kind: kind, QuoteCurrencyAssetID: &quoteAssetID, Precision: 8}
	if err := g.Create(a).Error; err != nil {
		t.Fatalf("seed asset %s/%s: %v", symbol, kind, err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Asset{}, a.ID) })
	return a.ID
}

// upsertPosition writes a (account, asset, quantity) row.
func upsertPosition(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, quantity string) {
	t.Helper()
	q, _ := decimal.NewFromString(quantity)
	p := &model.Position{UserID: userID, AccountID: accountID, AssetID: assetID, Quantity: q}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed position: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Position{}, p.ID) })
}

func insertPrice(t *testing.T, g *gorm.DB, assetID, quoteAssetID int64, price string, asOf time.Time) {
	t.Helper()
	pr, _ := decimal.NewFromString(price)
	p := &model.Price{AssetID: assetID, QuoteAssetID: quoteAssetID, Price: pr, AsOf: asOf, Source: "test"}
	if err := g.Create(p).Error; err != nil {
		t.Fatalf("seed price: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Price{}, p.ID) })
}

// seedHoldingTxn seeds a ledger transaction (asset + kind) so the household
// net-worth trend can fold quantity per asset over time (#282).
func seedHoldingTxn(t *testing.T, g *gorm.DB, userID, accountID, assetID int64, kind string, date time.Time, amount string) {
	t.Helper()
	tx := &model.Transaction{
		UserID: userID, AccountID: accountID, AssetID: assetID,
		Kind:            kind,
		Amount:          decimal.RequireFromString(amount),
		TransactionDate: date,
		Source:          "manual",
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed holding txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
}

// TestAggregator_Allocation rolls cash + equity into kind buckets and
// verifies private accounts are excluded.
func TestAggregator_Allocation(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	aapl := seedAsset(t, g, "AAPL-AL-"+fmt.Sprintf("%d", time.Now().UnixNano()), model.AssetKindEquity, usd)
	insertPrice(t, g, aapl, usd, "150", time.Now().Add(-time.Hour))

	ownerID := seedUser(t, g, "alloc-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Allocation", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	chk := seedAccount(t, g, ownerID, "chk")
	brk := seedAccount(t, g, ownerID, "brk")
	priv := seedAccount(t, g, ownerID, "private")
	// Shared
	upsertPosition(t, g, ownerID, chk.ID, usd, "1000")
	upsertPosition(t, g, ownerID, brk.ID, aapl, "10") // 10 × 150 = 1500
	// Private — must not show up anywhere
	upsertPosition(t, g, ownerID, priv.ID, usd, "9999")

	setShare(t, g, chk.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, brk.ID, hh.ID, model.VisibilityBalanceAndTxns)

	out, err := agg.Allocation(ctx, hh.ID)
	if err != nil {
		t.Fatalf("Allocation: %v", err)
	}
	byKind := map[string]string{}
	for _, b := range out {
		byKind[b.Kind] = b.Value
	}
	if byKind[model.AssetKindFiat] != "1000" {
		t.Errorf("fiat bucket = %q, want 1000", byKind[model.AssetKindFiat])
	}
	if byKind[model.AssetKindEquity] != "1500" {
		t.Errorf("equity bucket = %q, want 1500", byKind[model.AssetKindEquity])
	}
	for _, b := range out {
		if b.Value == "9999" {
			t.Errorf("private account leaked into allocation: %+v", b)
		}
	}
}

// TestAggregator_Allocation_UnpricedAssetIncomplete covers the #282 contract on
// allocation: an asset with no available price marks its kind bucket incomplete
// (value not silently inflated by a $0), while priced kinds stay complete.
func TestAggregator_Allocation_UnpricedAssetIncomplete(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	btc := testutil.LookupAssetID(t, g, "BTC", "crypto")
	ownerID := seedUser(t, g, "alloc-incomplete")
	hh := seedHouseholdRow(t, g, ownerID, "AllocIncomplete", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	acct := seedAccount(t, g, ownerID, "mixed")
	upsertPosition(t, g, ownerID, acct.ID, usd, "1000")
	upsertPosition(t, g, ownerID, acct.ID, btc, "0.5") // no BTC price → unpriced
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	out, err := agg.Allocation(ctx, hh.ID)
	if err != nil {
		t.Fatalf("Allocation: %v", err)
	}
	got := map[string]household.AssetClassAllocation{}
	for _, b := range out {
		got[b.Kind] = b
	}
	if fiat := got[model.AssetKindFiat]; fiat.Value != "1000" || !fiat.Complete {
		t.Errorf("fiat bucket = %+v, want {1000 complete}", fiat)
	}
	crypto, ok := got[model.AssetKindCrypto]
	if !ok {
		t.Fatalf("crypto bucket missing — an unpriced asset must still surface as its kind")
	}
	if crypto.Value != "0" || crypto.Complete {
		t.Errorf("crypto bucket = %+v, want {0 incomplete} (BTC unpriced, not silently valued)", crypto)
	}
}

// TestAggregator_Allocation_InGraceExcluded ensures a leaver's shared
// account stops contributing to allocation during grace.
func TestAggregator_Allocation_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	ownerID := seedUser(t, g, "alloc-grace-owner")
	leaverID := seedUser(t, g, "alloc-grace-leaver")

	hh := seedHouseholdRow(t, g, ownerID, "AllocGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "owner-chk")
	leaverAcct := seedAccount(t, g, leaverID, "leaver-chk")
	upsertPosition(t, g, ownerID, ownerAcct.ID, usd, "100")
	upsertPosition(t, g, leaverID, leaverAcct.ID, usd, "9999")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	out, err := agg.Allocation(ctx, hh.ID)
	if err != nil {
		t.Fatalf("Allocation: %v", err)
	}
	for _, b := range out {
		if b.Value == "9999" || b.Value == "10099" {
			t.Errorf("in-grace leaver leaked into allocation: %+v", b)
		}
	}
	// Owner's 100 fiat is what we expect.
	var fiat string
	for _, b := range out {
		if b.Kind == model.AssetKindFiat {
			fiat = b.Value
		}
	}
	if fiat != "100" {
		t.Errorf("fiat bucket = %q, want 100 (only owner)", fiat)
	}
}

// TestAggregator_AccountSummaries returns one row per shared account with
// balance + visibility, and excludes private accounts.
func TestAggregator_AccountSummaries(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	ownerID := seedUser(t, g, "summ-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Summaries", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	a1 := seedAccount(t, g, ownerID, "chk")
	a2 := seedAccount(t, g, ownerID, "sav")
	priv := seedAccount(t, g, ownerID, "private")
	upsertPosition(t, g, ownerID, a1.ID, usd, "200")
	upsertPosition(t, g, ownerID, a2.ID, usd, "300")
	upsertPosition(t, g, ownerID, priv.ID, usd, "9999")

	setShare(t, g, a1.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, a2.ID, hh.ID, model.VisibilityBalanceOnly)

	out, err := agg.AccountSummaries(ctx, hh.ID)
	if err != nil {
		t.Fatalf("AccountSummaries: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (private excluded); got %+v", len(out), out)
	}
	byID := map[int64]household.AccountSummary{}
	for _, s := range out {
		byID[s.AccountID] = s
	}
	if byID[a1.ID].Balance != "200" || byID[a1.ID].Visibility != model.VisibilityBalanceAndTxns {
		t.Errorf("a1 summary = %+v, want balance=200 visibility=balance_and_txns", byID[a1.ID])
	}
	if byID[a2.ID].Balance != "300" || byID[a2.ID].Visibility != model.VisibilityBalanceOnly {
		t.Errorf("a2 summary = %+v, want balance=300 visibility=balance_only", byID[a2.ID])
	}
	if _, leaked := byID[priv.ID]; leaked {
		t.Errorf("private account leaked into summaries")
	}
}

// TestAggregator_AccountSummaries_InGraceExcluded ensures leaver's shared
// account drops off during grace.
func TestAggregator_AccountSummaries_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	ownerID := seedUser(t, g, "summ-grace-owner")
	leaverID := seedUser(t, g, "summ-grace-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "SummGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "owner")
	leaverAcct := seedAccount(t, g, leaverID, "leaver")
	upsertPosition(t, g, ownerID, ownerAcct.ID, usd, "10")
	upsertPosition(t, g, leaverID, leaverAcct.ID, usd, "9999")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	out, err := agg.AccountSummaries(ctx, hh.ID)
	if err != nil {
		t.Fatalf("AccountSummaries: %v", err)
	}
	if len(out) != 1 || out[0].AccountID != ownerAcct.ID {
		t.Fatalf("out = %+v, want one entry for owner only", out)
	}
}

// TestAggregator_NetWorthTrend returns one point per day in the window
// and reflects positions × historical prices.
func TestAggregator_NetWorthTrend(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	ownerID := seedUser(t, g, "nwt-owner")
	hh := seedHouseholdRow(t, g, ownerID, "NWT", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	// Account holds 100 USD + 50 EUR from the start of the year (ledger fold,
	// constant quantity). EUR price appears mid-window and rises.
	acct := seedAccount(t, g, ownerID, "mixed")
	seedHoldingTxn(t, g, ownerID, acct.ID, usd, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "100")
	seedHoldingTxn(t, g, ownerID, acct.ID, eur, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "50")
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	// No EUR price in March; 1.0 in April, 2.0 in May.
	insertPrice(t, g, eur, usd, "1.0", time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC))
	insertPrice(t, g, eur, usd, "2.0", time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))

	// Freeze the clock so the month-end grid is deterministic.
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })

	out, err := agg.NetWorthTrend(ctx, hh.ID, 3) // Mar, Apr, May
	if err != nil {
		t.Fatalf("NetWorthTrend: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 month-end points", len(out))
	}
	// March: EUR unpriced → incomplete, USD-only 100.
	if out[0].Value != "100" || out[0].Complete {
		t.Errorf("March point = {%s complete:%v}, want {100 false} (EUR unpriced)", out[0].Value, out[0].Complete)
	}
	// April: 100 + 50×1.0 = 150. May: 100 + 50×2.0 = 200.
	if out[1].Value != "150" || !out[1].Complete {
		t.Errorf("April point = {%s complete:%v}, want {150 true}", out[1].Value, out[1].Complete)
	}
	if out[2].Value != "200" || !out[2].Complete {
		t.Errorf("May point = {%s complete:%v}, want {200 true}", out[2].Value, out[2].Complete)
	}
}

// TestAggregator_NetWorthTrend_InGraceExcluded covers (c) on the trend.
func TestAggregator_NetWorthTrend_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	ownerID := seedUser(t, g, "nwt-grace-owner")
	leaverID := seedUser(t, g, "nwt-grace-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "NWTGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "owner")
	leaverAcct := seedAccount(t, g, leaverID, "leaver")
	seedHoldingTxn(t, g, ownerID, ownerAcct.ID, usd, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "100")
	seedHoldingTxn(t, g, leaverID, leaverAcct.ID, usd, model.KindOpeningBalance, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "9999")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	out, err := agg.NetWorthTrend(ctx, hh.ID, 1)
	if err != nil {
		t.Fatalf("NetWorthTrend: %v", err)
	}
	for _, p := range out {
		if p.Value != "100" {
			t.Errorf("point %+v != 100 — in-grace leaver leaked", p)
		}
	}
}

// TestAggregator_AccountSummaries_FlagsStalePricing: an account holding an
// asset whose only price is older than the valuation stale window reports
// Complete=false; fresh-priced and primary-currency-only accounts report
// Complete=true (#339, #282 contract).
func TestAggregator_AccountSummaries_FlagsStalePricing(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	fresh := seedAsset(t, g, "FRSH-"+fmt.Sprintf("%d", time.Now().UnixNano()), model.AssetKindEquity, usd)
	stale := seedAsset(t, g, "STAL-"+fmt.Sprintf("%d", time.Now().UnixNano()), model.AssetKindEquity, usd)
	insertPrice(t, g, fresh, usd, "10", time.Now().Add(-time.Hour))
	insertPrice(t, g, stale, usd, "10", time.Now().Add(-30*24*time.Hour)) // outside DefaultStaleWindow

	ownerID := seedUser(t, g, "summ-stale-owner")
	hh := seedHouseholdRow(t, g, ownerID, "SummStale", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	cashAcct := seedAccount(t, g, ownerID, "cash-only")
	freshAcct := seedAccount(t, g, ownerID, "fresh-equity")
	staleAcct := seedAccount(t, g, ownerID, "stale-equity")
	upsertPosition(t, g, ownerID, cashAcct.ID, usd, "100")
	upsertPosition(t, g, ownerID, freshAcct.ID, fresh, "5")
	upsertPosition(t, g, ownerID, staleAcct.ID, stale, "5")
	setShare(t, g, cashAcct.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, freshAcct.ID, hh.ID, model.VisibilityBalanceOnly)
	setShare(t, g, staleAcct.ID, hh.ID, model.VisibilityBalanceOnly)

	out, err := agg.AccountSummaries(ctx, hh.ID)
	if err != nil {
		t.Fatalf("AccountSummaries: %v", err)
	}
	byID := map[int64]household.AccountSummary{}
	for _, s := range out {
		byID[s.AccountID] = s
	}
	if !byID[cashAcct.ID].Complete {
		t.Errorf("cash-only account Complete = false, want true (primary currency needs no price)")
	}
	if !byID[freshAcct.ID].Complete {
		t.Errorf("fresh-priced account Complete = false, want true")
	}
	if byID[staleAcct.ID].Complete {
		t.Errorf("stale-priced account Complete = true, want false (only price is 30d old)")
	}
}

// TestAggregator_Dashboard_NetWorthCompleteness: the household headline
// reports complete for fresh/primary-currency positions and partial when a
// shared position's only price predates the stale window (#344).
func TestAggregator_Dashboard_NetWorthCompleteness(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	ownerID := seedUser(t, g, "dash-nwc-owner")
	hh := seedHouseholdRow(t, g, ownerID, "DashNWC", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	cashAcct := seedAccount(t, g, ownerID, "cash")
	upsertPosition(t, g, ownerID, cashAcct.ID, usd, "500")
	setShare(t, g, cashAcct.ID, hh.ID, model.VisibilityBalanceOnly)

	dash, err := agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard: %v", err)
	}
	if dash.NetWorth != "500" || !dash.NetWorthComplete {
		t.Errorf("dashboard = {net_worth:%s complete:%v}, want {500 true}", dash.NetWorth, dash.NetWorthComplete)
	}

	// A stale-priced shared equity flips the headline to partial.
	stale := seedAsset(t, g, "DNWC-"+fmt.Sprintf("%d", time.Now().UnixNano()), model.AssetKindEquity, usd)
	insertPrice(t, g, stale, usd, "10", time.Now().Add(-30*24*time.Hour))
	brkAcct := seedAccount(t, g, ownerID, "brk")
	upsertPosition(t, g, ownerID, brkAcct.ID, stale, "5")
	setShare(t, g, brkAcct.ID, hh.ID, model.VisibilityBalanceOnly)

	dash, err = agg.Dashboard(ctx, hh.ID, household.PeriodCurrentMonth)
	if err != nil {
		t.Fatalf("Dashboard (stale): %v", err)
	}
	if dash.NetWorthComplete {
		t.Error("net_worth_complete = true, want false (shared equity priced 30d ago)")
	}
}

// seedMerchantTxnH mirrors service.seedMerchantTxn for the household
// package — a transaction carrying merchant/description fields, used by
// TestAggregator_TopMerchants*.
func seedMerchantTxnH(t *testing.T, g *gorm.DB, userID, accountID int64, merchant string, when time.Time, amount string, isTransfer bool) {
	t.Helper()
	amt, _ := decimal.NewFromString(amount)
	tx := &model.Transaction{
		UserID:          userID,
		AccountID:       accountID,
		MerchantName:    &merchant,
		Amount:          amt,
		Source:          "manual",
		TransactionDate: when,
		IsTransfer:      isTransfer,
	}
	if err := g.Create(tx).Error; err != nil {
		t.Fatalf("seed merchant txn: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
}

// TestAggregator_CategoryTrend_PrivacyAndTrailingAverage covers (a)/(b): a
// private account never contributes, a balance_only account contributes to
// nothing here (category trend needs txn-level visibility), and only the
// balance_and_txns account's spend feeds the month-over-month series —
// including the this-month vs. trailing-average comparison.
func TestAggregator_CategoryTrend_PrivacyAndTrailingAverage(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	ctxOwner := seedUser(t, g, "ctrend-owner")
	hh := seedHouseholdRow(t, g, ctxOwner, "CTrend", 30)
	addMember(t, g, hh.ID, ctxOwner, model.RoleOwner, nil)

	full := seedAccount(t, g, ctxOwner, "full")
	balOnly := seedAccount(t, g, ctxOwner, "bal-only")
	priv := seedAccount(t, g, ctxOwner, "private")
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)
	// priv: no share row at all.

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	cat := &model.Category{Name: "CTrendCat-" + suffix, Slug: "ctrend-cat-" + suffix}
	if err := g.Create(cat).Error; err != nil {
		t.Fatalf("seed category: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Category{}, cat.ID) })

	seedTxn(t, g, ctxOwner, full.ID, "-100", &cat.ID, time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, ctxOwner, full.ID, "-100", &cat.ID, time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, ctxOwner, full.ID, "-300", &cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	// balance_only + private spend must never surface.
	seedTxn(t, g, ctxOwner, balOnly.ID, "-9999", &cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, ctxOwner, priv.ID, "-9999", &cat.ID, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))

	items, err := agg.CategoryTrend(ctx, hh.ID, 3)
	if err != nil {
		t.Fatalf("CategoryTrend: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d categories, want 1; items=%+v", len(items), items)
	}
	if items[0].ThisMonth != "300" {
		t.Errorf("ThisMonth = %s, want 300 (balance_only/private spend must not leak)", items[0].ThisMonth)
	}
	if items[0].TrailingAverage != "100" {
		t.Errorf("TrailingAverage = %s, want 100", items[0].TrailingAverage)
	}
}

// TestAggregator_CategoryTrend_InGraceExcluded covers (c): an in-grace
// leaver's spend must not contribute to the live category trend.
func TestAggregator_CategoryTrend_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	ownerID := seedUser(t, g, "ctrend-grace-owner")
	leaverID := seedUser(t, g, "ctrend-grace-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "CTrendGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "ctrend-owner-acct")
	leaverAcct := seedAccount(t, g, leaverID, "ctrend-leaver-acct")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	seedTxn(t, g, ownerID, ownerAcct.ID, "-10", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, leaverID, leaverAcct.ID, "-9999", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))

	items, err := agg.CategoryTrend(ctx, hh.ID, 1)
	if err != nil {
		t.Fatalf("CategoryTrend: %v", err)
	}
	if len(items) != 1 || items[0].ThisMonth != "10" {
		t.Errorf("items = %+v, want one category with this_month=10 (in-grace leaver excluded)", items)
	}
}

// TestAggregator_TopMerchants_PrivacyAndGrouping covers (a)/(b): private and
// balance_only spend never surface; balance_and_txns spend groups by
// merchant with count + amount.
func TestAggregator_TopMerchants_PrivacyAndGrouping(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "merch-owner")
	hh := seedHouseholdRow(t, g, ownerID, "Merch", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	full := seedAccount(t, g, ownerID, "merch-full")
	balOnly := seedAccount(t, g, ownerID, "merch-bal-only")
	priv := seedAccount(t, g, ownerID, "merch-private")
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)

	when := time.Now().Add(-time.Hour)
	seedMerchantTxnH(t, g, ownerID, full.ID, "COSTCO", when, "-50", false)
	seedMerchantTxnH(t, g, ownerID, full.ID, "COSTCO", when, "-25", false)
	seedMerchantTxnH(t, g, ownerID, balOnly.ID, "COSTCO", when, "-9999", false)
	seedMerchantTxnH(t, g, ownerID, priv.ID, "COSTCO", when, "-9999", false)

	items, err := agg.TopMerchants(ctx, hh.ID, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopMerchants: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d merchants, want 1; items=%+v", len(items), items)
	}
	if items[0].Merchant != "COSTCO" || items[0].Amount != "75" || items[0].Count != 2 {
		t.Errorf("item = %+v, want {COSTCO 75 2} (balance_only/private spend must not leak)", items[0])
	}
}

// TestAggregator_TopMerchants_InGraceExcluded covers (c) for TopMerchants.
func TestAggregator_TopMerchants_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	ownerID := seedUser(t, g, "merch-grace-owner")
	leaverID := seedUser(t, g, "merch-grace-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "MerchGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "merch-owner-acct")
	leaverAcct := seedAccount(t, g, leaverID, "merch-leaver-acct")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	when := time.Now().Add(-time.Hour)
	seedMerchantTxnH(t, g, ownerID, ownerAcct.ID, "OWNERSHOP", when, "-10", false)
	seedMerchantTxnH(t, g, leaverID, leaverAcct.ID, "LEAVERSHOP", when, "-9999", false)

	items, err := agg.TopMerchants(ctx, hh.ID, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopMerchants: %v", err)
	}
	if len(items) != 1 || items[0].Merchant != "OWNERSHOP" {
		t.Errorf("items = %+v, want only OWNERSHOP (in-grace leaver excluded)", items)
	}
}

// TestAggregator_CashFlow_PrivacyAndZeroFill covers (a)/(b) plus the
// zero-fill contract: a quiet month still appears, balance_only/private
// spend never surfaces.
func TestAggregator_CashFlow_PrivacyAndZeroFill(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	ownerID := seedUser(t, g, "cf-owner")
	hh := seedHouseholdRow(t, g, ownerID, "CF", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)

	full := seedAccount(t, g, ownerID, "cf-full")
	balOnly := seedAccount(t, g, ownerID, "cf-bal-only")
	setShare(t, g, full.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, balOnly.ID, hh.ID, model.VisibilityBalanceOnly)

	seedTxn(t, g, ownerID, full.ID, "500", nil, time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, ownerID, full.ID, "-50", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, ownerID, balOnly.ID, "-9999", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))

	rows, err := agg.CashFlow(ctx, hh.ID, 3) // March, April, May
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	byMonth := map[string]household.CashFlowMonth{}
	for _, r := range rows {
		byMonth[r.Month] = r
	}
	may := byMonth["2026-05-01"]
	if may.Inflow != "500" || may.Outflow != "50" || may.Net != "450" {
		t.Errorf("May = %+v, want inflow=500 outflow=50 net=450 (balance_only spend excluded)", may)
	}
	apr := byMonth["2026-04-01"]
	if apr.Inflow != "0" || apr.Outflow != "0" {
		t.Errorf("April should be zero-padded, got %+v", apr)
	}
}

// TestAggregator_CashFlow_InGraceExcluded covers (c) for CashFlow.
func TestAggregator_CashFlow_InGraceExcluded(t *testing.T) {
	agg, g := newAggregator(t)
	ctx := context.Background()
	agg.SetClock(func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) })
	ownerID := seedUser(t, g, "cf-grace-owner")
	leaverID := seedUser(t, g, "cf-grace-leaver")
	hh := seedHouseholdRow(t, g, ownerID, "CFGrace", 30)
	addMember(t, g, hh.ID, ownerID, model.RoleOwner, nil)
	leftAt := time.Now().Add(-3 * 24 * time.Hour)
	addMember(t, g, hh.ID, leaverID, model.RoleContributor, &leftAt)

	ownerAcct := seedAccount(t, g, ownerID, "cf-owner-acct")
	leaverAcct := seedAccount(t, g, leaverID, "cf-leaver-acct")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	seedTxn(t, g, ownerID, ownerAcct.ID, "-10", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))
	seedTxn(t, g, leaverID, leaverAcct.ID, "-9999", nil, time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC))

	rows, err := agg.CashFlow(ctx, hh.ID, 1)
	if err != nil {
		t.Fatalf("CashFlow: %v", err)
	}
	if len(rows) != 1 || rows[0].Outflow != "10" {
		t.Errorf("rows = %+v, want outflow=10 (in-grace leaver excluded)", rows)
	}
}
