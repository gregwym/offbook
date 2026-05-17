package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Transaction struct {
	ID                    int64           `gorm:"primaryKey" json:"id"`
	UserID                int64           `gorm:"not null" json:"user_id"`
	AccountID             int64           `gorm:"not null" json:"account_id"`
	CategoryID            *int64          `json:"category_id,omitempty"`
	Amount                decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Currency              string          `gorm:"not null;default:USD" json:"currency"`
	Description           *string         `json:"description,omitempty"`
	DescriptionClean      *string         `json:"description_clean,omitempty"`
	MerchantName          *string         `json:"merchant_name,omitempty"`
	TransactionDate       time.Time       `gorm:"type:date;not null" json:"transaction_date"`
	PostedDate            *time.Time      `gorm:"type:date" json:"posted_date,omitempty"`
	Source                string          `gorm:"not null" json:"source"`
	ExternalID            *string         `gorm:"column:external_id" json:"external_id,omitempty"`
	PlaidTransactionID    *string         `gorm:"column:plaid_transaction_id" json:"plaid_transaction_id,omitempty"`
	CategorizationMethod  *string         `json:"categorization_method,omitempty"`
	IsTransfer            bool            `gorm:"not null;default:false" json:"is_transfer"`
	TransferPairID        *int64          `json:"transfer_pair_id,omitempty"`
	Notes                 *string         `json:"notes,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
	DeletedAt             gorm.DeletedAt  `gorm:"index" json:"-"`

	Account      *Account     `gorm:"foreignKey:AccountID" json:"-"`
	Category     *Category    `gorm:"foreignKey:CategoryID" json:"-"`
	TransferPair *Transaction `gorm:"foreignKey:TransferPairID" json:"-"`
}

func (Transaction) TableName() string { return "transactions" }
