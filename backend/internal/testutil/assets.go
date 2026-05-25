package testutil

import (
	"testing"

	"gorm.io/gorm"
)

// LookupAssetID returns the asset id for (symbol, kind). Used by tests
// that build model.Account / model.Transaction literals directly and
// need a real FK target for primary_quote_asset_id / asset_id.
//
// Migration 13 seeds USD/EUR/GBP/JPY/CNY/CAD/AUD/CHF/HKD/SGD as 'fiat'
// and BTC/ETH as 'crypto'. Anything else must be created explicitly.
func LookupAssetID(t *testing.T, g *gorm.DB, symbol, kind string) int64 {
	t.Helper()
	var id int64
	if err := g.Raw(`SELECT id FROM assets WHERE symbol = ? AND kind = ?`, symbol, kind).Scan(&id).Error; err != nil {
		t.Fatalf("lookup asset %s/%s: %v", symbol, kind, err)
	}
	if id == 0 {
		t.Fatalf("asset not found: symbol=%s kind=%s (migration 13 may not have run)", symbol, kind)
	}
	return id
}

// LookupUSDAssetID is shorthand for the most common case.
func LookupUSDAssetID(t *testing.T, g *gorm.DB) int64 {
	return LookupAssetID(t, g, "USD", "fiat")
}
