import { apiClient, type ApiItem } from './client'
import type { DashboardPeriod, DashboardSummary } from '../types/dashboard'

export async function getDashboardSummary(period: DashboardPeriod): Promise<DashboardSummary> {
  const res = await apiClient.get<ApiItem<DashboardSummary>>('/dashboard/summary', {
    params: { period },
  })
  return res.data.data
}
