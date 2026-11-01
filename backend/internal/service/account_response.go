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
	// UnpricedAssets names the holdings behind BalanceComplete=false — each
	// asset whose price chain was stale or missing, once — so the UI can say
	// which holding needs a price instead of only "partial" (#530). Always a
	// non-nil slice: [] when the balance is complete.
	UnpricedAssets []UnpricedAsset `json:"unpriced_assets"`

	// LastObservedAt/LastObservedSource surface the most recent
	// account_balance_observations row (ADR-0017) across the account's
	// positions — the "as of" provenance #369 adds so the UI can show when
	// a balance was last confirmed against the institution, independent of
	// LastSyncedAt (which tracks the plaid_item's transaction sync, not a
	// balance/holdings observation). Both nil for accounts with no recorded
	// observation (manual accounts; a Plaid account never yet reconciled).
	LastObservedAt     *time.Time `json:"last_observed_at"`
	LastObservedSource *string    `json:"last_observed_source"`
	// LastObservedBalance is the figure that observation reported, signed
	// like Balance, so the UI can say what was reported rather than pairing
	// the observation's date with today's derived balance (#673). Set only
	// when the observation is of the account's primary quote asset (its cash
	// balance); nil for a holding-quantity observation or no observation.
	LastObservedBalance *decimal.Decimal `json:"last_observed_balance"`

	// NeedsAttention is true when the account holds at least one
	// unacknowledged adjustment transaction whose delta is large relative to
	// the ledger fold immediately before it (#370) — surfaced on Accounts/
	// Insights until the user reviews it via the reconciliation view. False
	// (never nil) when no reconciliation wiring is present, matching
	// BalanceComplete's "no wiring means don't claim a signal we can't back"
	// contract inverted for a boolean flag: default to "nothing to flag".
	NeedsAttention bool `json:"needs_attention"`

	// AttentionCount is how many unacknowledged above-threshold adjustments
	// raised NeedsAttention (0 when it is false) — #697.
	AttentionCount int `json:"attention_count"`
	// AttentionAmount is the magnitude of the single flagged adjustment, in
	// the account's currency, so the Accounts list can say "Unexplained
	// adjustment of 20.00 USD". Nil when nothing is flagged, when several
	// adjustments are, or when the adjustment is in a holding's units.
	AttentionAmount *decimal.Decimal `json:"attention_amount"`
}

// UnpricedAsset identifies a holding whose price chain was stale or missing
// (#530). Symbol and kind are instance-wide reference data, not PII.
type UnpricedAsset struct {
	AssetID int64  `json:"asset_id"`
	Symbol  string `json:"symbol"`
	Kind    string `json:"kind"`
}
