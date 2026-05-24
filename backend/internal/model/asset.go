package model

import "time"

const (
	AssetKindFiat      = "fiat"
	AssetKindEquity    = "equity"
	AssetKindFund      = "fund"
	AssetKindCrypto    = "crypto"
	AssetKindBond      = "bond"
	AssetKindCommodity = "commodity"
	AssetKindOther     = "other"
)

// Asset is a unit of value tracked by the system. Fiat currencies (USD,
// EUR), securities (AAPL, VTSAX), and crypto (BTC) all live here.
//
// QuoteCurrencyAssetID points at the fiat asset this asset is normally
// priced in (e.g. AAPL → USD). NULL for fiat assets themselves.
type Asset struct {
	ID                   int64     `gorm:"primaryKey" json:"id"`
	Symbol               string    `gorm:"not null" json:"symbol"`
	Kind                 string    `gorm:"not null" json:"kind"`
	DisplayName          *string   `json:"display_name,omitempty"`
	QuoteCurrencyAssetID *int64    `gorm:"column:quote_currency_asset_id" json:"quote_currency_asset_id,omitempty"`
	Precision            int16     `gorm:"not null;default:8" json:"precision"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (Asset) TableName() string { return "assets" }
