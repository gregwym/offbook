package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type CategorizationRule struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	UserID     int64  `gorm:"not null;index" json:"user_id"`
	Pattern    string `gorm:"not null" json:"pattern"`
	CategoryID int64  `gorm:"not null" json:"category_id"`
	MatchType  string `gorm:"not null" json:"match_type"`
	Priority   int    `gorm:"not null;default:0" json:"priority"`
	IsActive   bool   `gorm:"not null;default:true" json:"is_active"`
	// AssetID, when non-nil, narrows the rule to transactions whose
	// asset_id matches. Combined (AND) with the text matcher. NULL keeps
	// the rule asset-agnostic — the M2-era behavior.
	AssetID *int64 `json:"asset_id,omitempty"`
	// RenameMerchant, when non-nil, is written to merchant_name on every
	// row this rule wins (#534). NULL leaves the merchant alone.
	RenameMerchant *string `json:"rename_merchant,omitempty"`
	// HideMatches hides the flow rows this rule wins from spending, income,
	// budgets and merchant reports (#536). A rule never unhides.
	HideMatches bool `gorm:"not null;default:false" json:"hide_matches"`
	// MarkReviewed marks the flow rows this rule wins reviewed, so they skip
	// the Needs review inbox (#538). A rule never un-reviews.
	MarkReviewed bool `gorm:"not null;default:false" json:"mark_reviewed"`
	// AddTags are added to the flow rows this rule wins, skipping tags the
	// row already has (#546). A rule never removes a tag.
	AddTags Tags `gorm:"type:text[];not null;default:'{}'" json:"add_tags"`
	// AccountID, when non-nil, narrows the rule to transactions in that
	// account (#550). NULL matches every account.
	AccountID *int64 `json:"account_id,omitempty"`
	// AmountMin / AmountMax, when non-nil, bound the transaction's
	// magnitude (|amount|), inclusive (#550). NULL leaves that side open.
	AmountMin *decimal.Decimal `gorm:"type:numeric(30,18)" json:"amount_min,omitempty"`
	AmountMax *decimal.Decimal `gorm:"type:numeric(30,18)" json:"amount_max,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	DeletedAt gorm.DeletedAt   `gorm:"index" json:"-"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"-"`
	Asset    *Asset    `gorm:"foreignKey:AssetID" json:"-"`
}

func (CategorizationRule) TableName() string { return "categorization_rules" }
