package model

import (
	"time"

	"gorm.io/gorm"
)

// AIThread is the renamed ai_conversations table with multi-tenant columns.
// shared_with_household = true makes a thread visible to other household
// members via the household aggregator's AIContext.
type AIThread struct {
	ID                  int64          `gorm:"primaryKey" json:"id"`
	UserID              int64          `gorm:"not null" json:"user_id"`
	HouseholdID         *int64         `json:"household_id,omitempty"`
	SharedWithHousehold bool           `gorm:"not null;default:false" json:"shared_with_household"`
	Title               *string        `json:"title,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`

	Messages []AIMessage `gorm:"foreignKey:ThreadID" json:"-"`
}

func (AIThread) TableName() string { return "ai_threads" }
