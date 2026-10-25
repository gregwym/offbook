import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  AssetClassAllocation,
  BudgetAlert,
  CashFlowMonth,
  CategoryTrendItem,
  DashboardPeriod,
  DashboardSummary,
  MerchantSpendItem,
  NetWorthPoint,
  RecurringItem,
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

// window is a [from, to) pair of YYYY-MM-DD dates; omitted = current month.
export type DateWindow = { from: string; to: string }

export async function getSpendByCategory(window?: DateWindow): Promise<SpendByCategoryItem[]> {
  const res = await apiClient.get<ApiList<SpendByCategoryItem>>('/dashboard/spend-by-category', {
    params: window,
  })
  return res.data.data
}

// month (YYYY-MM) anchors the window's last month (#609); omitted = now.
export async function getCashFlow(months = 12, month?: string): Promise<CashFlowMonth[]> {
  const res = await apiClient.get<ApiList<CashFlowMonth>>('/dashboard/cash-flow', { params: { months, month } })
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

export async function getCategoryTrend(months = 6): Promise<CategoryTrendItem[]> {
  const res = await apiClient.get<ApiList<CategoryTrendItem>>('/dashboard/category-trend', {
    params: { months },
  })
  return res.data.data
}

export async function getTopMerchants(limit = 10, window?: DateWindow): Promise<MerchantSpendItem[]> {
  const res = await apiClient.get<ApiList<MerchantSpendItem>>('/dashboard/top-merchants', {
    params: { limit, ...window },
  })
  return res.data.data
}

export async function getRecurring(): Promise<RecurringItem[]> {
  const res = await apiClient.get<ApiList<RecurringItem>>('/dashboard/recurring')
  return res.data.data
}
