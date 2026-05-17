// DashboardSummary mirrors backend service.DashboardSummary. Money fields
// are decimal strings; format via AmountDisplay.
export type DashboardSummary = {
  period: { key: string; from: string; to: string }
  net_worth: string
  income: string
  spending: string
  account_count: number
  transaction_count: number
  by_category: Array<{ category_id: number | null; name: string; amount: string }>
}

export const DASHBOARD_PERIODS = ['current_month', 'last_30d', 'ytd'] as const
export type DashboardPeriod = (typeof DASHBOARD_PERIODS)[number]
