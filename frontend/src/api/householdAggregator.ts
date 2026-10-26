import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  BudgetPaceItem,
  CategorySpendingItem,
  GoalProgressItem,
  HouseholdAccountSummary,
  HouseholdAssetClassAllocation,
  HouseholdCashFlowMonth,
  HouseholdCategoryTrendItem,
  HouseholdDashboard,
  HouseholdMerchantSpendItem,
  HouseholdNetWorthPoint,
  HouseholdPeriodKey,
} from '../types/householdAggregator'
import type { BudgetMonthSummary } from '../types/budget'
import type { DateWindow } from './dashboard'

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

// Spending by category over shared balance_and_txns accounts (#619);
// window omitted = the current month.
export async function getHouseholdSpendByCategory(window?: DateWindow): Promise<CategorySpendingItem[]> {
  const res = await apiClient.get<ApiList<CategorySpendingItem>>('/h/insights/spend-by-category', {
    params: window,
  })
  return res.data.data
}

// month (YYYY-MM) anchors the window's last month (#619); omitted = now.
export async function getHouseholdCategoryTrend(months = 6, month?: string): Promise<HouseholdCategoryTrendItem[]> {
  const res = await apiClient.get<ApiList<HouseholdCategoryTrendItem>>('/h/insights/category-trend', {
    params: { months, month },
  })
  return res.data.data
}

export async function getHouseholdTopMerchants(limit = 10, window?: DateWindow): Promise<HouseholdMerchantSpendItem[]> {
  const res = await apiClient.get<ApiList<HouseholdMerchantSpendItem>>('/h/insights/top-merchants', {
    params: { limit, ...window },
  })
  return res.data.data
}

// month (YYYY-MM) anchors the window's last month (#619); omitted = now.
export async function getHouseholdCashFlow(months = 6, month?: string): Promise<HouseholdCashFlowMonth[]> {
  const res = await apiClient.get<ApiList<HouseholdCashFlowMonth>>('/h/insights/cash-flow', {
    params: { months, month },
  })
  return res.data.data
}

// The current month's shared-budget totals (#589).
export async function getHouseholdBudgetSummary(): Promise<BudgetMonthSummary> {
  const res = await apiClient.get<ApiItem<BudgetMonthSummary>>('/h/budgets/summary')
  return res.data.data
}
