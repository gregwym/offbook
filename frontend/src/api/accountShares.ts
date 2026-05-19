import { apiClient, type ApiItem, type ApiList } from './client'

// Visibility values mirror backend model.VisibilityPrivate etc. 'private'
// is the API-only sentinel for "clear the row" — never stored.
export type AccountShareVisibility = 'private' | 'balance_only' | 'balance_and_txns'

export type AccountShare = {
  id: number
  account_id: number
  household_id: number
  visibility: AccountShareVisibility
  created_at: string
  updated_at: string
}

export async function listAccountShares(accountID: number): Promise<AccountShare[]> {
  const res = await apiClient.get<ApiList<AccountShare>>(`/accounts/${accountID}/shares`)
  return res.data.data
}

// setAccountShare returns null when visibility === 'private' (server clears
// the row + responds 204). Otherwise the new share row.
export async function setAccountShare(
  accountID: number,
  householdID: number,
  visibility: AccountShareVisibility,
): Promise<AccountShare | null> {
  const res = await apiClient.put<ApiItem<AccountShare> | ''>(
    `/accounts/${accountID}/shares/${householdID}`,
    { visibility },
  )
  if (res.status === 204 || typeof res.data === 'string') return null
  return res.data.data
}
