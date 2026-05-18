// PlaidItem mirrors backend model.PlaidItem (JSON tags). The encrypted
// access_token is intentionally absent — backend serializes `json:"-"` on it.
// `null` for optional pointer fields preserves explicit-null semantics.
export type PlaidItem = {
  id: number
  user_id: number
  plaid_item_id: string
  institution_id?: string | null
  institution_name?: string | null
  status: string
  last_synced_at?: string | null
  last_sync_error?: string | null
  last_sync_status: 'never' | 'syncing' | 'ok' | 'error'
  created_at: string
  updated_at: string
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
}
