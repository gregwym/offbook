package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Budget struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	UserID     int64           `gorm:"not null" json:"user_id"`
	CategoryID int64           `gorm:"not null" json:"category_id"`
	Period     string          `gorm:"not null" json:"period"`
	Amount     decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Rollover   bool            `gorm:"not null;default:false" json:"rollover"`
	// IsActive: the DB column has DEFAULT TRUE (migration 000001), but we
	// deliberately leave the GORM tag without `default:true` so explicit
	// `false` from the service layer survives INSERT — gorm omits zero-
	// valued fields when the tag carries a default clause.
	IsActive  bool           `gorm:"not null" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"-"`
}

func (Budget) TableName() string { return "budgets" }
