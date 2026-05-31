package valuation_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/testutil"
)

// TestValuePositions_PartialPricingReportsIncomplete proves the #282 contract:
// a position with no available price is reported as unpriced, not silently
// counted as $0. The priced legs still sum; the unpriced asset is surfaced.
func TestValuePositions_PartialPricingReportsIncomplete(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")
	btc := testutil.LookupAssetID(t, g, "BTC", "crypto")

	insertPrice(t, g, eur, usd, "1.10", time.Now().Add(-time.Hour))
	// Deliberately no BTC price.

	positions := []model.Position{
		{AssetID: usd, Quantity: decimal.NewFromInt(1000)},
		{AssetID: eur, Quantity: decimal.NewFromInt(100)},
		{AssetID: btc, Quantity: decimal.RequireFromString("0.5")},
	}
	got, err := svc.ValuePositions(ctx, positions, time.Now(), usd)
	if err != nil {
		t.Fatalf("ValuePositions: %v", err)
	}
	if !got.Value.Equal(decimal.RequireFromString("1110")) {
		t.Errorf("Value = %s, want 1110 (1000 USD + 100×1.10 EUR; BTC excluded)", got.Value)
	}
	if got.Complete() {
		t.Error("Complete() = true, want false (BTC unpriced)")
	}
	if len(got.Unpriced) != 1 || got.Unpriced[0] != btc {
		t.Errorf("Unpriced = %v, want [%d] (BTC)", got.Unpriced, btc)
	}
}

// TestValuePositions_AllPricedIsComplete: when every position prices, the total
// is the whole story and Complete() is true.
func TestValuePositions_AllPricedIsComplete(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")

	insertPrice(t, g, eur, usd, "1.25", time.Now().Add(-time.Hour))

	positions := []model.Position{
		{AssetID: usd, Quantity: decimal.NewFromInt(500)},
		{AssetID: eur, Quantity: decimal.NewFromInt(200)},
	}
	got, err := svc.ValuePositions(ctx, positions, time.Now(), usd)
	if err != nil {
		t.Fatalf("ValuePositions: %v", err)
	}
	if !got.Value.Equal(decimal.RequireFromString("750")) {
		t.Errorf("Value = %s, want 750 (500 + 200×1.25)", got.Value)
	}
	if !got.Complete() || len(got.Unpriced) != 0 {
		t.Errorf("Complete=%v Unpriced=%v, want complete with no unpriced", got.Complete(), got.Unpriced)
	}
}

// TestValuePositions_HistoricalQuantityUsesAsOfPrice: the same set valued at an
// earlier asOf picks up that date's price, so a synthetic position carrying a
// past fold quantity values at the past price — the basis for the unified trend.
func TestValuePositions_HistoricalQuantityUsesAsOfPrice(t *testing.T) {
	svc, g := newSvc(t)
	ctx := context.Background()
	usd := testutil.LookupUSDAssetID(t, g)
	eur := testutil.LookupAssetID(t, g, "EUR", "fiat")

	now := time.Now()
	insertPrice(t, g, eur, usd, "1.00", now.AddDate(0, 0, -60))
	insertPrice(t, g, eur, usd, "1.50", now.AddDate(0, 0, -1))

	positions := []model.Position{{AssetID: eur, Quantity: decimal.NewFromInt(100)}}

	// Disable the stale gate so an old price is acceptable for history.
	hist := svc.WithStaleWindow(0)

	past, err := hist.ValuePositions(ctx, positions, now.AddDate(0, 0, -30), usd)
	if err != nil {
		t.Fatalf("ValuePositions past: %v", err)
	}
	if !past.Value.Equal(decimal.NewFromInt(100)) {
		t.Errorf("past Value = %s, want 100 (100×1.00 at -30d)", past.Value)
	}

	recent, err := hist.ValuePositions(ctx, positions, now, usd)
	if err != nil {
		t.Fatalf("ValuePositions recent: %v", err)
	}
	if !recent.Value.Equal(decimal.NewFromInt(150)) {
		t.Errorf("recent Value = %s, want 150 (100×1.50)", recent.Value)
	}
}
