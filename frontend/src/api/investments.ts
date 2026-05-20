import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  CreateInvestmentInput,
  Investment,
  PortfolioSummary,
} from '../types/investment'

export async function listLatestHoldings(): Promise<Investment[]> {
  const res = await apiClient.get<ApiList<Investment>>('/investments')
  // The backend fix in #180 returns `[]` for the empty case, but normalize
  // here too so stale containers or future regressions don't crash the page
  // (`holdings.length` reads further down the call stack).
  return res.data.data ?? []
}

export async function listSnapshotHistory(
  accountID: number,
  ticker: string,
): Promise<Investment[]> {
  const res = await apiClient.get<ApiList<Investment>>('/investments', {
    params: { account_id: accountID, ticker },
  })
  return res.data.data ?? []
}

export async function createInvestment(
  input: CreateInvestmentInput,
): Promise<Investment> {
  const res = await apiClient.post<ApiItem<Investment>>('/investments', input)
  return res.data.data
}

export async function getPortfolioSummary(): Promise<PortfolioSummary> {
  const res = await apiClient.get<ApiItem<PortfolioSummary>>('/investments/portfolio')
  return res.data.data
}
