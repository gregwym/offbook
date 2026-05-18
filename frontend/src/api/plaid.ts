import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  ExchangeResponse,
  LinkTokenResponse,
  PlaidItem,
  SyncAccountsResponse,
  SyncTransactionsResponse,
} from '../types/plaid'

// createLinkToken issues a one-shot link_token for Plaid Link. Tokens are
// safe to discard after one open() call; the page re-fetches on retry.
export async function createLinkToken(): Promise<LinkTokenResponse> {
  const res = await apiClient.post<ApiItem<LinkTokenResponse>>('/plaid/link/token')
  return res.data.data
}

// exchangePublicToken hands Plaid's public_token to the backend so it can
// trade for a durable access_token (encrypted server-side). The public_token
// must never be logged or persisted client-side.
export async function exchangePublicToken(publicToken: string): Promise<ExchangeResponse> {
  const res = await apiClient.post<ApiItem<ExchangeResponse>>('/plaid/link/exchange', {
    public_token: publicToken,
  })
  return res.data.data
}

export async function syncAccounts(itemID: string): Promise<SyncAccountsResponse> {
  const res = await apiClient.post<ApiItem<SyncAccountsResponse>>(
    `/plaid/items/${encodeURIComponent(itemID)}/sync-accounts`,
  )
  return res.data.data
}

export async function syncTransactions(itemID: string): Promise<SyncTransactionsResponse> {
  const res = await apiClient.post<ApiItem<SyncTransactionsResponse>>(
    `/plaid/items/${encodeURIComponent(itemID)}/sync-transactions`,
  )
  return res.data.data
}

export async function listItems(): Promise<{ items: PlaidItem[]; total: number }> {
  const res = await apiClient.get<ApiList<PlaidItem>>('/plaid/items')
  return { items: res.data.data, total: res.data.total }
}

export async function disconnectItem(itemID: string): Promise<void> {
  await apiClient.delete(`/plaid/items/${encodeURIComponent(itemID)}`)
}
