package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// AccountBalanceObservation is an append-only record of what a sync source
// (Plaid, CSV, …) reported as the holding of AssetID in AccountID at AsOf
// (ADR-0017). It is the reconciliation input + audit trail, NOT the quantity
// source of truth — the transaction ledger is. ReconcilePosition compares the
// observed quantity to the transaction fold and writes the reconciling
// opening_balance/adjustment transaction.
type AccountBalanceObservation struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	UserID           int64           `gorm:"not null" json:"user_id"`
	AccountID        int64           `gorm:"not null" json:"account_id"`
	AssetID          int64           `gorm:"not null" json:"asset_id"`
	ObservedQuantity decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"observed_quantity"`
	AsOf             time.Time       `gorm:"not null" json:"as_of"`
	Source           string          `gorm:"not null" json:"source"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (AccountBalanceObservation) TableName() string { return "account_balance_observations" }
