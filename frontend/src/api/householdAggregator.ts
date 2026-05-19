import { apiClient, type ApiItem } from './client'
import type { HouseholdDashboard, HouseholdPeriodKey } from '../types/householdAggregator'

export async function getHouseholdDashboard(
  period: HouseholdPeriodKey = 'current_month',
): Promise<HouseholdDashboard> {
  const res = await apiClient.get<ApiItem<HouseholdDashboard>>('/h/dashboard', {
    params: { period },
  })
  return res.data.data
}
