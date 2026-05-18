// PlaidItem mirrors backend model.PlaidItem (JSON tags). The encrypted
// access_token is intentionally absent — backend serializes `json:"-"` on it.
// `null` for optional pointer fields preserves explicit-null semantics.
// `unresolved_sync_errors` is populated only by GET /plaid/items (the
// list endpoint joins it in to avoid an N+1) — see ADR-0011.
export type PlaidItem = {
  id: number
  user_id: number
  plaid_item_id: string
  institution_id?: string | null
  institution_name?: string | null
  status: string
  last_synced_at?: string | null
  last_sync_error?: string | null
  last_sync_status: 'never' | 'syncing' | 'ok' | 'ok_with_errors' | 'error'
  unresolved_sync_errors?: number
  created_at: string
  updated_at: string
}

// PlaidSyncError mirrors backend model.PlaidSyncError. raw_payload is the
// original Plaid transaction object — `unknown` rather than a typed shape
// because the whole point of the DLQ is to preserve whatever Plaid sent,
// including fields we don't know yet.
export type PlaidSyncError = {
  id: number
  user_id: number
  plaid_item_id: number
  plaid_transaction_id?: string | null
  raw_payload: unknown
  error_code: string
  error_message: string
  occurred_at: string
  resolved_at?: string | null
  resolution?: 'retried_ok' | 'dismissed' | null
}

// LinkTokenResponse — POST /plaid/link/token.
export type LinkTokenResponse = {
  link_token: string
  expiration: string
}

// ExchangeResponse — POST /plaid/link/exchange. `item_id` is the Plaid
// item_id (string) used to route subsequent /plaid/items/:item_id/... calls.
export type ExchangeResponse = {
  id: number
  item_id: string
  institution?: string | null
  status: string
}

// SyncAccountsResponse / SyncTransactionsResponse — narrow counts only;
// no account or transaction details cross the wire here.
export type SyncAccountsResponse = {
  created: number
  updated: number
}

export type SyncTransactionsResponse = {
  inserted: number
  modified: number
  removed: number
  failed: number
}
