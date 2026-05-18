package model

import (
	"encoding/json"
	"time"
)

// PlaidSyncError is a DLQ row for a single Plaid transaction that failed
// to import during a /transactions/sync drain. The raw payload is kept so
// the owner can retry without re-syncing the whole item.
//
// Hard-delete only (no soft-delete / no gorm.DeletedAt) — DLQ rows are
// audit material; "dismissed" is recorded via Resolution, not deletion.
type PlaidSyncError struct {
	ID                 int64   `gorm:"primaryKey" json:"id"`
	UserID             int64   `gorm:"not null" json:"user_id"`
	PlaidItemID        int64   `gorm:"column:plaid_item_id;not null" json:"plaid_item_id"`
	PlaidTransactionID *string `gorm:"column:plaid_transaction_id" json:"plaid_transaction_id,omitempty"`
	// RawPayload is the original Plaid row JSON. JSONB in Postgres; we
	// keep it as json.RawMessage so handlers can echo it back unmodified.
	RawPayload   json.RawMessage `gorm:"column:raw_payload;type:jsonb;not null" json:"raw_payload"`
	ErrorCode    string          `gorm:"column:error_code;not null" json:"error_code"`
	ErrorMessage string          `gorm:"column:error_message;not null" json:"error_message"`
	OccurredAt   time.Time       `gorm:"column:occurred_at;not null;default:now()" json:"occurred_at"`
	// ResolvedAt + Resolution are paired: both NULL = unresolved; both
	// non-NULL after Retry or Dismiss. The CHECK in 000007 enforces this.
	ResolvedAt *time.Time `gorm:"column:resolved_at" json:"resolved_at,omitempty"`
	Resolution *string    `gorm:"column:resolution" json:"resolution,omitempty"`
}

func (PlaidSyncError) TableName() string { return "plaid_sync_errors" }

// Resolution values for PlaidSyncError.Resolution.
const (
	ResolutionRetriedOK = "retried_ok"
	ResolutionDismissed = "dismissed"
)

// Error codes recorded in PlaidSyncError.ErrorCode. Free-form text is
// allowed in the column, but stick to these for known failure modes so
// the UI can group/filter.
const (
	PlaidSyncErrorCodeDecimalOverflow = "DECIMAL_OVERFLOW"
	PlaidSyncErrorCodeValidation      = "VALIDATION"
	PlaidSyncErrorCodeMapping         = "MAPPING"
	PlaidSyncErrorCodeDB              = "DB"
	PlaidSyncErrorCodeUnknown         = "UNKNOWN"
)
