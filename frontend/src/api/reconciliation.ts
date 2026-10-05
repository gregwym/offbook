import { apiClient, type ApiItem } from './client'
import type { ReconciliationReport } from '../types/reconciliation'
import type { Transaction } from '../types/transaction'

export async function getReconciliation(accountId: number): Promise<ReconciliationReport> {
  const res = await apiClient.get<ApiItem<ReconciliationReport>>(`/accounts/${accountId}/reconciliation`)
  return res.data.data
}

export async function acknowledgeAdjustment(transactionId: number, note?: string): Promise<Transaction> {
  const res = await apiClient.patch<ApiItem<Transaction>>(`/transactions/${transactionId}/acknowledge`, { note })
  return res.data.data
}
