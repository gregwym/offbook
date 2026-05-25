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

	// Account holds 100 USD + 50 EUR. EUR price history shifts mid-window.
	acct := seedAccount(t, g, ownerID, "mixed")
	upsertPosition(t, g, ownerID, acct.ID, usd, "100")
	upsertPosition(t, g, ownerID, acct.ID, eur, "50")
	setShare(t, g, acct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	// 60 days ago: EUR 1.0 → net worth 150.
	// 10 days ago: EUR 2.0 → net worth 200.
	insertPrice(t, g, eur, usd, "1.0", time.Now().Add(-60*24*time.Hour))
	insertPrice(t, g, eur, usd, "2.0", time.Now().Add(-10*24*time.Hour))

	// Freeze the clock so the window is deterministic.
	now := time.Now().UTC()
	agg.SetClock(func() time.Time { return now })

	out, err := agg.NetWorthTrend(ctx, hh.ID, 3) // 3 months
	if err != nil {
		t.Fatalf("NetWorthTrend: %v", err)
	}
	if len(out) < 30 {
		t.Fatalf("len(out) = %d, want at least 30 points", len(out))
	}
	// First point: prior to any price → 100 (USD only).
	if out[0].Value != "100" {
		t.Errorf("first point = %q, want 100 (EUR has no price yet → contributes 0)", out[0].Value)
	}
	// Some middle point after the 1.0 price → 150.
	var saw150, saw200 bool
	for _, p := range out {
		if p.Value == "150" {
			saw150 = true
		}
		if p.Value == "200" {
			saw200 = true
		}
	}
	if !saw150 {
		t.Errorf("trend never reaches 150 (EUR=1.0 era)")
	}
	if !saw200 {
		t.Errorf("trend never reaches 200 (EUR=2.0 era)")
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
	upsertPosition(t, g, ownerID, ownerAcct.ID, usd, "100")
	upsertPosition(t, g, leaverID, leaverAcct.ID, usd, "9999")
	setShare(t, g, ownerAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)
	setShare(t, g, leaverAcct.ID, hh.ID, model.VisibilityBalanceAndTxns)

	now := time.Now().UTC()
	agg.SetClock(func() time.Time { return now })
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
