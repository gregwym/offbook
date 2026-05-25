import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  BudgetPaceItem,
  GoalProgressItem,
  HouseholdAccountSummary,
  HouseholdAssetClassAllocation,
  HouseholdDashboard,
  HouseholdNetWorthPoint,
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

export async function getGoalProgress(): Promise<GoalProgressItem[]> {
  const res = await apiClient.get<ApiList<GoalProgressItem>>('/h/goals/progress')
  return res.data.data
}

export async function getHouseholdAllocation(): Promise<HouseholdAssetClassAllocation[]> {
  const res = await apiClient.get<ApiList<HouseholdAssetClassAllocation>>('/h/insights/allocation')
  return res.data.data
}

export async function getHouseholdNetWorthTrend(months = 12): Promise<HouseholdNetWorthPoint[]> {
  const res = await apiClient.get<ApiList<HouseholdNetWorthPoint>>('/h/insights/net-worth', {
    params: { months },
  })
  return res.data.data
}

export async function getHouseholdAccountSummaries(): Promise<HouseholdAccountSummary[]> {
  const res = await apiClient.get<ApiList<HouseholdAccountSummary>>('/h/insights/accounts')
  return res.data.data
}
