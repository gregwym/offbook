package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Position is the current (account × asset) holding. Quantity is the fact;
// market value derives from quantity × the latest price for asset_id in the
// user's primary currency.
//
// CostBasis is the running average-cost basis in the user's primary
// currency. NULL when unknown (e.g. positions backfilled from holdings
// snapshots that didn't carry cost basis).
type Position struct {
	ID        int64            `gorm:"primaryKey" json:"id"`
	UserID    int64            `gorm:"not null" json:"user_id"`
	AccountID int64            `gorm:"not null" json:"account_id"`
	AssetID   int64            `gorm:"not null" json:"asset_id"`
	Quantity  decimal.Decimal  `gorm:"type:numeric(30,18);not null" json:"quantity"`
	CostBasis *decimal.Decimal `gorm:"type:numeric(30,18)" json:"cost_basis,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
	DeletedAt gorm.DeletedAt   `gorm:"index" json:"-"`
}

func (Position) TableName() string { return "positions" }
