package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Account struct {
	ID                  int64           `gorm:"primaryKey" json:"id"`
	UserID              int64           `gorm:"not null" json:"user_id"`
	Name                string          `gorm:"not null" json:"name"`
	InstitutionSlug     string          `gorm:"not null" json:"institution_slug"`
	AccountType         string          `gorm:"not null" json:"account_type"`
	Currency            string          `gorm:"not null;default:USD" json:"currency"`
	Balance             decimal.Decimal `gorm:"type:numeric(30,18);not null;default:0" json:"balance"`
	PrimaryQuoteAssetID int64           `gorm:"column:primary_quote_asset_id;not null" json:"primary_quote_asset_id"`
	LastFour            *string         `json:"last_four,omitempty"`
	PlaidAccountID      *string         `gorm:"column:plaid_account_id" json:"plaid_account_id,omitempty"`
	PlaidItemID         *string         `gorm:"column:plaid_item_id" json:"plaid_item_id,omitempty"`
	IsActive            bool            `gorm:"not null;default:true" json:"is_active"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	DeletedAt           gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (Account) TableName() string { return "accounts" }
