// Account mirrors backend model.Account. Money fields arrive as decimal
// strings (NUMERIC(30,18)) — never parse into Number; format via AmountDisplay.
//
// The three last_sync_* fields are joined from the underlying plaid_items
// row server-side (#65). They are `null` for accounts not linked to Plaid
// (manual entries) — the frontend uses that as the signal to hide the pill.
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
  last_four?: string | null
  plaid_account_id?: string | null
  plaid_item_id?: string | null
  is_active: boolean
  created_at: string
  updated_at: string
  last_sync_status: SyncStatus | null
  last_synced_at: string | null
  last_sync_error: string | null
}

// SyncStatus mirrors the CHECK constraint in migration 000006 on
// plaid_items.last_sync_status.
export const SYNC_STATUSES = ['never', 'syncing', 'ok', 'error'] as const
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
