package model

import "time"

// AICategorizationVerdict is one cached merchant→category decision from the
// AI transaction categorizer (ADR-0022 §6). One row per (user, merchant_key);
// upserted on every AI call so a recurring merchant is priced once, not once
// per transaction. Also the source list for the frontend's "promote to rule"
// affordance.
type AICategorizationVerdict struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	UserID      int64     `gorm:"not null" json:"user_id"`
	MerchantKey string    `gorm:"not null" json:"merchant_key"`
	CategoryID  int64     `gorm:"not null" json:"category_id"`
	Confidence  float64   `gorm:"type:numeric(5,4);not null" json:"confidence"`
	Provider    string    `gorm:"not null" json:"provider"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
}

func (AICategorizationVerdict) TableName() string { return "ai_categorization_verdicts" }
