package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Budget struct {
	ID         int64           `gorm:"primaryKey" json:"id"`
	CategoryID int64           `gorm:"not null" json:"category_id"`
	Period     string          `gorm:"not null" json:"period"`
	Amount     decimal.Decimal `gorm:"type:numeric(30,18);not null" json:"amount"`
	Rollover   bool            `gorm:"not null;default:false" json:"rollover"`
	IsActive   bool            `gorm:"not null;default:true" json:"is_active"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	DeletedAt  gorm.DeletedAt  `gorm:"index" json:"-"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"-"`
}

func (Budget) TableName() string { return "budgets" }
