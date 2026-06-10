import { apiClient, type ApiItem } from './client'

// RefreshResult mirrors backend prices.RefreshResult (ADR-0014 Phase 1).
// `skipped` lists held symbols no provider could quote — those assets keep
// their stale/partial flags until a covering provider lands.
export type PriceRefreshResult = {
  refreshed: number
  skipped: string[]
  as_of: string
}

// refreshPrices is user-initiated by design: the click is the egress
// consent. Only the user's held symbols are sent upstream.
export async function refreshPrices(): Promise<PriceRefreshResult> {
  const res = await apiClient.post<ApiItem<PriceRefreshResult>>('/prices/refresh')
  return res.data.data
}
