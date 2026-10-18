package model

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
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
	// Import-history fields (#376). ContentHash is the hex SHA-256 of the
	// uploaded bytes (duplicate-statement detection); Reconciled is nil when the
	// document carried no total to compare against.
	ContentHash   *string          `json:"content_hash,omitempty"`
	RowsDuplicate *int             `json:"rows_duplicate,omitempty"`
	RowsError     *int             `json:"rows_error,omitempty"`
	RowSum        *decimal.Decimal `gorm:"type:numeric(30,18)" json:"row_sum,omitempty"`
	DocTotals     json.RawMessage  `gorm:"type:jsonb" json:"doc_totals,omitempty"`
	Reconciled    *bool            `json:"reconciled,omitempty"`
	// UndoneAt / RowsUndone record an undo of the import: the rows it created
	// were soft-deleted. An undone job no longer counts as a completed import.
	UndoneAt   *time.Time `json:"undone_at,omitempty"`
	RowsUndone *int       `json:"rows_undone,omitempty"`
	// Extraction holds the staged ImportResult preview (rows + totals + CSV
	// echo) for AI jobs awaiting commit. JSONB; nil for deterministic jobs.
	Extraction  json.RawMessage `gorm:"type:jsonb" json:"extraction,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`

	Account *Account `gorm:"foreignKey:AccountID" json:"-"`
}

func (IngestionJob) TableName() string { return "ingestion_jobs" }
