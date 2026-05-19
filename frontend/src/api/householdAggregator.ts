import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  BudgetPaceItem,
  HouseholdDashboard,
  HouseholdPeriodKey,
} from '../types/householdAggregator'

export async function getHouseholdDashboard(
  period: HouseholdPeriodKey = 'current_month',
): Promise<HouseholdDashboard> {
  const res = await apiClient.get<ApiItem<HouseholdDashboard>>('/h/dashboard', {
    params: { period },
  })
  return res.data.data
}

export async function getBudgetPace(
  period: HouseholdPeriodKey = 'current_month',
): Promise<BudgetPaceItem[]> {
  const res = await apiClient.get<ApiList<BudgetPaceItem>>('/h/budgets/pace', {
    params: { period },
  })
  return res.data.data
}
