package service

import (
	"time"

	"github.com/gregwym/offbook/backend/internal/model"
)

// AccountResponse is the JSON shape returned from account read endpoints.
//
// It embeds the raw model.Account so existing client fields stay byte-stable
// (#65 is purely additive on the wire) and layers on three sync-status
// fields joined from the underlying plaid_items row.
//
// For non-Plaid accounts, all three sync fields are nil and marshal as
// `null` (not omitted) so the frontend can treat the wire shape uniformly:
// "if last_sync_status === null, this account isn't Plaid-linked".
type AccountResponse struct {
	*model.Account
	LastSyncStatus *string    `json:"last_sync_status"`
	LastSyncedAt   *time.Time `json:"last_synced_at"`
	LastSyncError  *string    `json:"last_sync_error"`
}
