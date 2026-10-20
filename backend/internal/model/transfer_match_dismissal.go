package model

import (
	"time"

	"gorm.io/gorm"
)

// TransferMatchDismissal records that the user said a proposed transfer pair
// (#371) is "not a transfer" (#510). Stored as an unordered pair, lower id
// first, so the matcher and spend_lines can look it up either way round.
type TransferMatchDismissal struct {
	ID                int64          `gorm:"primaryKey" json:"id"`
	UserID            int64          `gorm:"not null" json:"-"`
	TransactionLowID  int64          `gorm:"not null" json:"transaction_low_id"`
	TransactionHighID int64          `gorm:"not null" json:"transaction_high_id"`
	CreatedAt         time.Time      `json:"created_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TransferMatchDismissal) TableName() string { return "transfer_match_dismissals" }
