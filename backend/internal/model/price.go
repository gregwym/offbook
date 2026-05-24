package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Price is one observation of (asset, quote_asset, as_of, price). Append-only.
// Source identifies where the observation came from — 'plaid', 'manual',
// 'historical_snapshot' (from the M6 investments backfill), and later
// 'yahoo'/'ecb'/'coingecko' when the Tier-3 provider lands (ADR-0014).
type Price struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	AssetID      int64           `gorm:"not null" json:"asset_id"`
	QuoteAssetID int64           `gorm:"column:quote_asset_id;not null" json:"quote_asset_id"`
	AsOf         time.Time       `gorm:"not null" json:"as_of"`
	Price        decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"price"`
	Source       string          `gorm:"not null" json:"source"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (Price) TableName() string { return "prices" }
