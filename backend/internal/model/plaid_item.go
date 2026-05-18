package model

import (
	"time"

	"gorm.io/gorm"
)

// PlaidItem is one user's link to one Plaid Item (≈ one institution
// connection). The bearer access_token is stored encrypted in
// AccessTokenEnc; never expose it through any GET endpoint.
//
// Soft-deletable: revoke/disconnect flips deleted_at so the row is
// invisible to API queries but stays around for historical reconciliation
// until a hard purge.
type PlaidItem struct {
	ID              int64      `gorm:"primaryKey" json:"id"`
	UserID          int64      `gorm:"not null" json:"user_id"`
	PlaidItemID     string     `gorm:"column:plaid_item_id;not null" json:"plaid_item_id"`
	AccessTokenEnc  []byte     `gorm:"column:access_token_enc;type:bytea;not null" json:"-"`
	InstitutionID   *string    `gorm:"column:institution_id" json:"institution_id,omitempty"`
	InstitutionName *string    `gorm:"column:institution_name" json:"institution_name,omitempty"`
	Status          string     `gorm:"not null;default:active" json:"status"`
	Cursor          *string    `json:"-"`
	LastSyncedAt    *time.Time `gorm:"column:last_synced_at" json:"last_synced_at,omitempty"`
	LastSyncError   *string    `gorm:"column:last_sync_error" json:"last_sync_error,omitempty"`
	// LastSyncStatus: 'never' | 'syncing' | 'ok' | 'error'. The lifecycle
	// is owned by service/plaid — handlers should not write this directly.
	LastSyncStatus string         `gorm:"column:last_sync_status;not null;default:never" json:"last_sync_status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (PlaidItem) TableName() string { return "plaid_items" }
