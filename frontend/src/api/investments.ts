import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  CreateInvestmentInput,
  Investment,
  PortfolioSummary,
} from '../types/investment'

export async function listLatestHoldings(): Promise<Investment[]> {
  const res = await apiClient.get<ApiList<Investment>>('/investments')
  return res.data.data
}

export async function listSnapshotHistory(
  accountID: number,
  ticker: string,
): Promise<Investment[]> {
  const res = await apiClient.get<ApiList<Investment>>('/investments', {
    params: { account_id: accountID, ticker },
  })
  return res.data.data
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
