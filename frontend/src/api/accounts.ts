import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  Account,
  AccountPII,
  CreateAccountInput,
  UpdateAccountInput,
} from '../types/account'

export type AccountListFilter = {
  institution_slug?: string
  account_type?: string
  is_active?: boolean
  limit?: number
  offset?: number
}

export async function listAccounts(f: AccountListFilter = {}): Promise<{ items: Account[]; total: number }> {
  const params: Record<string, string | number | boolean> = {}
  if (f.institution_slug) params.institution_slug = f.institution_slug
  if (f.account_type) params.account_type = f.account_type
  if (f.is_active !== undefined) params.is_active = f.is_active
  if (f.limit !== undefined) params.limit = f.limit
  if (f.offset !== undefined) params.offset = f.offset
  const res = await apiClient.get<ApiList<Account>>('/accounts', { params })
  return { items: res.data.data, total: res.data.total }
}

export async function getAccount(id: number): Promise<Account> {
  const res = await apiClient.get<ApiItem<Account>>(`/accounts/${id}`)
  return res.data.data
}

export async function createAccount(input: CreateAccountInput): Promise<Account> {
  const res = await apiClient.post<ApiItem<Account>>('/accounts', input)
  return res.data.data
}

export async function updateAccount(id: number, input: UpdateAccountInput): Promise<Account> {
  const res = await apiClient.patch<ApiItem<Account>>(`/accounts/${id}`, input)
  return res.data.data
}

export async function deleteAccount(id: number): Promise<void> {
  await apiClient.delete(`/accounts/${id}`)
}

// PII reads/writes always hit a separate endpoint, by design — never bundled
// into account responses. Mask in the UI; the network tab is auditable.
export async function getAccountPII(id: number): Promise<AccountPII> {
  const res = await apiClient.get<ApiItem<AccountPII>>(`/accounts/${id}/pii`)
  return res.data.data
}

export async function updateAccountPII(id: number, fields: AccountPII): Promise<AccountPII> {
  const res = await apiClient.put<ApiItem<AccountPII>>(`/accounts/${id}/pii`, fields)
  return res.data.data
}
