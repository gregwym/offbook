import { apiClient, type ApiItem } from './client'
import type { RecordTradeInput, TradeRecord } from '../types/trade'

export async function recordTrade(
  accountID: number,
  input: RecordTradeInput,
): Promise<TradeRecord> {
  const res = await apiClient.post<ApiItem<TradeRecord>>(
    `/accounts/${accountID}/trades`,
    input,
  )
  return res.data.data
}
