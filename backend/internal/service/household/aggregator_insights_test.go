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
