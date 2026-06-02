package model

import (
	"encoding/json"
	"time"
)

// IngestionJob is the per-import audit trail and, for AI imports, the staging
// store (ADR-0019 §7). One row per import attempt: deterministic CSV writes a
// completed row; AI extraction writes an 'extracted' row holding the staged
// rows in Extraction, applied verbatim on commit so the provider never re-runs.
type IngestionJob struct {
	ID     int64  `gorm:"primaryKey" json:"id"`
	UserID int64  `gorm:"not null" json:"user_id"`
	Source string `gorm:"not null" json:"source"` // csv | pdf | photo
	// Extractor records how rows were produced; Provider names the AI provider
	// (nil for deterministic). ConsentedAt is set when the user consented to a
	// cloud egress.
	Extractor    *string    `json:"extractor,omitempty"` // deterministic | ai
	Provider     *string    `json:"provider,omitempty"`  // claude | ollama
	ConsentedAt  *time.Time `json:"consented_at,omitempty"`
	AccountID    *int64     `json:"account_id,omitempty"`
	FileName     *string    `json:"file_name,omitempty"`
	Status       string     `gorm:"not null;default:pending" json:"status"` // pending|processing|extracted|completed|failed
	RowsTotal    *int       `json:"rows_total,omitempty"`
	RowsImported *int       `json:"rows_imported,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	// Extraction holds the staged ImportResult preview (rows + totals + CSV
	// echo) for AI jobs awaiting commit. JSONB; nil for deterministic jobs.
	Extraction  json.RawMessage `gorm:"type:jsonb" json:"extraction,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`

	Account *Account `gorm:"foreignKey:AccountID" json:"-"`
}

func (IngestionJob) TableName() string { return "ingestion_jobs" }
