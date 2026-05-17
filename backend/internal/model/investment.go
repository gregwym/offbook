package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type Investment struct {
	ID           int64            `gorm:"primaryKey" json:"id"`
	UserID       int64            `gorm:"not null" json:"user_id"`
	AccountID    int64            `gorm:"not null" json:"account_id"`
	Ticker       string           `gorm:"not null" json:"ticker"`
	Name         *string          `json:"name,omitempty"`
	AssetClass   *string          `json:"asset_class,omitempty"`
	Quantity     decimal.Decimal  `gorm:"type:numeric(30,18);not null" json:"quantity"`
	CostBasis    *decimal.Decimal `gorm:"type:numeric(30,18)" json:"cost_basis,omitempty"`
	MarketValue  *decimal.Decimal `gorm:"type:numeric(30,18)" json:"market_value,omitempty"`
	SnapshotDate time.Time        `gorm:"type:date;not null" json:"snapshot_date"`
	Source       string           `gorm:"not null" json:"source"`
	CreatedAt    time.Time        `json:"created_at"`

	Account *Account `gorm:"foreignKey:AccountID" json:"-"`
}

func (Investment) TableName() string { return "investments" }
