package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Transaction kinds (ADR-0017). `kind` classifies a row for both quantity
// reconstruction (positions = Σ amount per account/asset) and flow analytics.
// Only KindFlow is counted by spending / cash-flow / budget aggregates;
// KindOpeningBalance and KindAdjustment are quantity facts but not flow.
const (
	KindFlow           = "flow"
	KindTradeLeg       = "trade_leg"
	KindOpeningBalance = "opening_balance"
	KindAdjustment     = "adjustment"
)

// Transaction is a movement of `Amount` units of `AssetID` in or out of
// `AccountID`. Per ADR-0013, `Amount` is the quantity (positive = in,
// negative = out) and `AssetID` is the unit. For cash transactions
// `AssetID` equals the parent account's primary_quote_asset. For trades
// (issue #238), the cash leg and security leg each carry their own
// `AssetID` and a shared `TransferPairID`. `Kind` (ADR-0017) classifies
// the row; it defaults to KindFlow.
type Transaction struct {
	ID                   int64           `gorm:"primaryKey" json:"id"`
	UserID               int64           `gorm:"not null" json:"user_id"`
	AccountID            int64           `gorm:"not null" json:"account_id"`
	AssetID              int64           `gorm:"not null" json:"asset_id"`
	CategoryID           *int64          `json:"category_id,omitempty"`
	Kind                 string          `gorm:"not null;default:flow" json:"kind"`
	Amount               decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Description          *string         `json:"description,omitempty"`
	DescriptionClean     *string         `json:"description_clean,omitempty"`
	MerchantName         *string         `json:"merchant_name,omitempty"`
	TransactionDate      time.Time       `gorm:"type:date;not null" json:"transaction_date"`
	PostedDate           *time.Time      `gorm:"type:date" json:"posted_date,omitempty"`
	Source               string          `gorm:"not null" json:"source"`
	ExternalID           *string         `gorm:"column:external_id" json:"external_id,omitempty"`
	PlaidTransactionID   *string         `gorm:"column:plaid_transaction_id" json:"plaid_transaction_id,omitempty"`
	CategorizationMethod *string         `json:"categorization_method,omitempty"`
	CategorizationRuleID *int64          `json:"categorization_rule_id,omitempty"`
	IsTransfer           bool            `gorm:"not null;default:false" json:"is_transfer"`
	TransferPairID       *int64          `json:"transfer_pair_id,omitempty"`
	Notes                *string         `json:"notes,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	DeletedAt            gorm.DeletedAt  `gorm:"index" json:"-"`

	Account      *Account     `gorm:"foreignKey:AccountID" json:"-"`
	Asset        *Asset       `gorm:"foreignKey:AssetID" json:"-"`
	Category     *Category    `gorm:"foreignKey:CategoryID" json:"-"`
	TransferPair *Transaction `gorm:"foreignKey:TransferPairID" json:"-"`
}

func (Transaction) TableName() string { return "transactions" }
