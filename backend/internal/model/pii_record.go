package model

import "time"

// PIIRecord is a row in pii_store — the ONLY table that holds PII.
// Only pii_repo may read/write this model (see ARCHITECTURE.md "PII Isolation").
type PIIRecord struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	EntityType string    `gorm:"not null" json:"entity_type"`
	EntityID   int64     `gorm:"not null" json:"entity_id"`
	FieldName  string    `gorm:"not null" json:"field_name"`
	Value      string    `gorm:"not null" json:"value"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (PIIRecord) TableName() string { return "pii_store" }
