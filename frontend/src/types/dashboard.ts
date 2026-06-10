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

// BudgetAlert mirrors backend service.BudgetAlert. pct is uncapped — over-
// budget rows can report > 1.0 (e.g. 1.30 = 30% over).
export type BudgetAlert = {
  budget_id: number
  category_id: number
  category_name: string
  period: string
  spent: string
  limit: string
  pct: number
  severity: 'warning' | 'over'
}

// Chart payload types — mirror service.SpendByCategoryItem, CashFlowMonth,
// NetWorthPoint. Money fields are decimal strings; parse to Number only at
// the chart boundary.
export type SpendByCategoryItem = {
  category_id: number | null
  name: string
  color?: string
  amount: string
}

export type CashFlowMonth = {
  month: string // YYYY-MM-DD (first of month)
  inflow: string
  outflow: string
  net: string
}

export type NetWorthPoint = {
  date: string // YYYY-MM-DD (month-end)
  total: string
  // false when an asset held at this month-end had no available price, so
  // `total` is a partial sum (#282).
  complete: boolean
}

// AssetClassAllocation mirrors backend service.AssetClassAllocation (#341).
// Wire-identical to the household aggregator's allocation row, so the
// Insights band renders both scopes from one shape.
export type AssetClassAllocation = {
  kind: string
  value: string
  // false when a position of this kind had no fresh price — `value` is a
  // partial sum (#282).
  complete: boolean
}
