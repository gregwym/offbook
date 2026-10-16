package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// TransactionSplit allocates part of a flow transaction's amount to a
// category (#438). Splits carry the parent's sign and sum exactly to the
// parent amount; they never alter the ledger quantity.
type TransactionSplit struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	TransactionID int64           `gorm:"not null" json:"transaction_id"`
	UserID        int64           `gorm:"not null" json:"-"`
	CategoryID    *int64          `json:"category_id,omitempty"`
	Amount        decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Note          *string         `json:"note,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (TransactionSplit) TableName() string { return "transaction_splits" }
