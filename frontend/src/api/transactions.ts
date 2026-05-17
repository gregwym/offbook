import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  CreateTransactionInput,
  Transaction,
  UpdateTransactionInput,
} from '../types/transaction'

export type TransactionListFilter = {
  account_id?: number
  // Pass null for uncategorized-only (mapped to ?category_id=null on the wire).
  category_id?: number | null
  from?: string  // YYYY-MM-DD
  to?: string
  search?: string
  limit?: number
  offset?: number
}

export async function listTransactions(
  f: TransactionListFilter = {},
): Promise<{ items: Transaction[]; total: number }> {
  const params: Record<string, string | number> = {}
  if (f.account_id !== undefined) params.account_id = f.account_id
  if (f.category_id !== undefined) {
    params.category_id = f.category_id === null ? 'null' : f.category_id
  }
  if (f.from) params.from = f.from
  if (f.to) params.to = f.to
  if (f.search) params.search = f.search
  if (f.limit !== undefined) params.limit = f.limit
  if (f.offset !== undefined) params.offset = f.offset

  const res = await apiClient.get<ApiList<Transaction>>('/transactions', { params })
  return { items: res.data.data, total: res.data.total }
}

export async function createTransaction(input: CreateTransactionInput): Promise<Transaction> {
  const res = await apiClient.post<ApiItem<Transaction>>('/transactions', input)
  return res.data.data
}

export async function updateTransaction(id: number, input: UpdateTransactionInput): Promise<Transaction> {
  const res = await apiClient.patch<ApiItem<Transaction>>(`/transactions/${id}`, input)
  return res.data.data
}

export async function deleteTransaction(id: number): Promise<void> {
  await apiClient.delete(`/transactions/${id}`)
}
