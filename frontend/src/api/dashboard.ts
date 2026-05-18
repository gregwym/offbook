import { apiClient, type ApiItem, type ApiList } from './client'
import type { BudgetAlert, DashboardPeriod, DashboardSummary } from '../types/dashboard'

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
