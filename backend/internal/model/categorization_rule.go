package model

import (
	"time"

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
	AssetID   *int64         `json:"asset_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"-"`
	Asset    *Asset    `gorm:"foreignKey:AssetID" json:"-"`
}

func (CategorizationRule) TableName() string { return "categorization_rules" }
