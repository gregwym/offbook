package model

import "time"

type IngestionJob struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	Source       string     `gorm:"not null" json:"source"`
	AccountID    *int64     `json:"account_id,omitempty"`
	FileName     *string    `json:"file_name,omitempty"`
	Status       string     `gorm:"not null;default:pending" json:"status"`
	RowsTotal    *int       `json:"rows_total,omitempty"`
	RowsImported *int       `json:"rows_imported,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`

	Account *Account `gorm:"foreignKey:AccountID" json:"-"`
}

func (IngestionJob) TableName() string { return "ingestion_jobs" }
