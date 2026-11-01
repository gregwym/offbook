// Account mirrors backend model.Account. Money fields arrive as decimal
// strings (NUMERIC(30,18)) — never parse into Number; format via AmountDisplay.
//
// The three last_sync_* fields are joined from the underlying plaid_items
// row server-side (#65). They are `null` for accounts not linked to Plaid
// (manual entries) — the frontend uses that as the signal to hide the pill.
// UnpricedAsset mirrors backend service.UnpricedAsset: a holding whose price
// was stale or missing when the balance was valued.
export type UnpricedAsset = {
  asset_id: number
  symbol: string
  kind: string
  // Household headline only (#601): the currency the owning member values
  // it in. Personal scope uses DashboardSummary.net_worth_currency instead.
  quote?: string
  quote_asset_id?: number
}

export type Account = {
  id: number
  user_id: number
  name: string
  institution_slug: string
  account_type: AccountType
  currency: string
  // primary_quote_asset_id is the cash sleeve's asset for this account
  // (per ADR-0013). Derived server-side from the chosen currency on
  // create — clients shouldn't try to mutate it.
  primary_quote_asset_id: number
  // balance is derived server-side from positions × prices (ADR-0013) on
  // GET responses. balance_complete=false means some position had no fresh
  // price, so balance is a partial sum — never treat it as the full value.
  balance: string
  balance_complete: boolean
  // unpriced_assets names each holding behind balance_complete=false (#530)
  // so the UI can say which one needs a price. [] when complete.
  unpriced_assets?: UnpricedAsset[]
  last_four?: string | null
  plaid_account_id?: string | null
  plaid_item_id?: string | null
  is_active: boolean
  created_at: string
  updated_at: string
  last_sync_status: SyncStatus | null
  last_synced_at: string | null
  last_sync_error: string | null
  // last_observed_at/last_observed_source join the most recent
  // account_balance_observations row (ADR-0017) across the account's
  // positions (#369) — when a balance/holdings figure was last confirmed
  // against the institution, distinct from last_synced_at (which tracks the
  // transaction sync, not a balance observation). Null for accounts with no
  // recorded observation (manual accounts; a Plaid account never reconciled).
  last_observed_at: string | null
  last_observed_source: string | null
  // last_observed_balance is the figure that observation reported, signed
  // like balance (#673). Null unless it was a cash-balance observation.
  last_observed_balance?: string | null
  // needs_attention is true when the account holds at least one
  // unacknowledged adjustment transaction whose delta is large relative to
  // the ledger fold immediately before it (#370). Review it on the
  // account's /accounts/:id/reconciliation view.
  needs_attention: boolean
  // #697: why. attention_count is how many unacknowledged adjustments are
  // flagged; attention_amount is the single flagged adjustment's magnitude
  // in the account's currency (decimal string) — null when several are
  // flagged or the adjustment is in a holding's units.
  attention_count?: number
  attention_amount?: string | null
}

// SyncStatus mirrors the CHECK constraint in migration 000027 on
// plaid_items.last_sync_status (widened from the original 4-state enum by
// #364's reauth_required + the earlier ok_with_errors DLQ terminus).
export const SYNC_STATUSES = ['never', 'syncing', 'ok', 'ok_with_errors', 'error', 'reauth_required'] as const
export type SyncStatus = (typeof SYNC_STATUSES)[number]

// Mirrors the CHECK constraint in migration 000001 + service.validAccountTypes.
export const ACCOUNT_TYPES = [
  'checking',
  'savings',
  'credit_card',
  'loan',
  'investment',
  'crypto',
  'cash',
  'other',
] as const
export type AccountType = (typeof ACCOUNT_TYPES)[number]

// accountTypeLabel turns the wire enum into the words shown to the user
// ("credit_card" → "Credit card").
export function accountTypeLabel(t: string): string {
  const words = t.replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

// isLiabilityType: balances on these accounts are what the user owes, stored
// negative. Forms take the amount owed as a positive figure (lib/liabilityEntry).
export function isLiabilityType(t: string): boolean {
  return t === 'credit_card' || t === 'loan'
}

// CreateAccountRequest mirrors backend handler.createAccountRequest. Balance
// is a decimal string when sent over the wire.
export type CreateAccountInput = {
  name: string
  institution_slug: string
  account_type: AccountType
  currency: string
  balance?: string
  last_four?: string | null
  is_active?: boolean
}

// UpdateAccountInput is a sparse patch; only provided fields are mutated.
// balance is excluded: it only exists on create (as the opening position);
// afterwards balance is derived and changed via transactions/trades.
export type UpdateAccountInput = Partial<Omit<CreateAccountInput, 'balance'>>

// AccountPII mirrors the map[string]string returned by GET /accounts/:id/pii.
// The allowlist matches backend service.allowedAccountPIIFields.
export const PII_FIELDS = ['holder_name', 'account_number', 'routing_number', 'address'] as const
export type AccountPIIField = (typeof PII_FIELDS)[number]
export type AccountPII = Partial<Record<AccountPIIField, string>>

// POST /accounts/:id/balance (#504): a user-reported balance for a manual
// account. balance is signed like Account.balance; as_of is YYYY-MM-DD.
export type UpdateBalanceInput = {
  balance: string
  as_of?: string
}

// recorded is the ledger row the update wrote (opening_balance on the first
// reconcile, adjustment after); null when the ledger already matched.
export type UpdateBalanceResult = {
  account: Account
  recorded: { id: number; kind: 'opening_balance' | 'adjustment'; amount: string } | null
}
