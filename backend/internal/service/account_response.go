package service

import (
	"time"

	"github.com/shopspring/decimal"

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

	// Balance is the valuation-derived account value — Σ positions × prices
	// in the account's primary quote asset, at response time. ADR-0013
	// dropped the stored scalar, so this is the only balance the API serves
	// (#291). Marshals as a decimal string.
	Balance decimal.Decimal `json:"balance"`
	// BalanceComplete is false when any position's price chain was stale or
	// missing, i.e. Balance is a partial sum rather than the whole story —
	// the #282 "no wrong-but-confident totals" contract.
	BalanceComplete bool `json:"balance_complete"`
}
