import { apiClient, type ApiItem, type ApiList } from './client'
import type { Transaction } from '../types/transaction'

// TransferCandidate is a proposed (unconfirmed) is_transfer pair (#371) —
// two of the user's own flow transactions with opposite amounts in
// different accounts. Nothing is linked until the user confirms.
export type TransferCandidate = {
  transaction_a: Transaction
  transaction_b: Transaction
}

export async function listTransferCandidates(): Promise<{ items: TransferCandidate[]; total: number }> {
  const res = await apiClient.get<ApiList<TransferCandidate>>('/transfers/candidates')
  return { items: res.data.data, total: res.data.total }
}

export async function confirmTransferMatch(
  transactionIDA: number,
  transactionIDB: number,
): Promise<TransferCandidate> {
  const res = await apiClient.post<ApiItem<TransferCandidate>>('/transfers/confirm', {
    transaction_id_a: transactionIDA,
    transaction_id_b: transactionIDB,
  })
  return res.data.data
}

// dismissTransferMatch records that a proposed pair is "not a transfer"
// (#510): it stops being proposed and both rows count in income/spending.
export async function dismissTransferMatch(transactionIDA: number, transactionIDB: number): Promise<void> {
  await apiClient.post('/transfers/dismiss', {
    transaction_id_a: transactionIDA,
    transaction_id_b: transactionIDB,
  })
}

export async function unlinkTransfer(transactionID: number): Promise<void> {
  await apiClient.post(`/transactions/${transactionID}/unlink-transfer`)
}
