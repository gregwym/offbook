import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  AssetClassAllocation,
  BudgetAlert,
  CashFlowMonth,
  DashboardPeriod,
  DashboardSummary,
  NetWorthPoint,
  SpendByCategoryItem,
} from '../types/dashboard'

export async function getDashboardSummary(period: DashboardPeriod): Promise<DashboardSummary> {
  const res = await apiClient.get<ApiItem<DashboardSummary>>('/dashboard/summary', {
    params: { period },
  })
  return res.data.data
}

export async function getBudgetAlerts(): Promise<BudgetAlert[]> {
  const res = await apiClient.get<ApiList<BudgetAlert>>('/dashboard/budget-alerts')
  return res.data.data
}

export async function getSpendByCategory(): Promise<SpendByCategoryItem[]> {
  const res = await apiClient.get<ApiList<SpendByCategoryItem>>('/dashboard/spend-by-category')
  return res.data.data
}

export async function getCashFlow(months = 12): Promise<CashFlowMonth[]> {
  const res = await apiClient.get<ApiList<CashFlowMonth>>('/dashboard/cash-flow', { params: { months } })
  return res.data.data
}

export async function getNetWorth(months = 12): Promise<NetWorthPoint[]> {
  const res = await apiClient.get<ApiList<NetWorthPoint>>('/dashboard/net-worth', { params: { months } })
  return res.data.data
}

export async function getAllocation(): Promise<AssetClassAllocation[]> {
  const res = await apiClient.get<ApiList<AssetClassAllocation>>('/dashboard/allocation')
  return res.data.data
}
