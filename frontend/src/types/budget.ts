// Mirrors backend model.Budget + service.BudgetSpend.
export const BUDGET_PERIODS = ['monthly', 'weekly', 'annual'] as const
export type BudgetPeriod = (typeof BUDGET_PERIODS)[number]

export type Budget = {
  id: number
  // Owned by exactly one of user_id (personal) / household_id (ADR-0018).
  // Personal-scope responses from this client always carry a user_id.
  user_id: number
  household_id: number | null
  category_id: number
  period: BudgetPeriod
  amount: string
  rollover: boolean
  // Starting carryover in the creation period (#508); ignored unless rollover.
  rollover_start: string
  is_active: boolean
  created_at: string
  updated_at: string
}

export type BudgetSpend = {
  budget_id: number
  category_id: number
  period: BudgetPeriod
  period_start: string
  period_end: string
  limit: string
  // Unspent balance rolled in from earlier periods ("0" unless rollover is on).
  carryover: string
  available: string
  spent: string
  remaining: string
  pct: number
}

// Month totals for the Budgets page (#589), personal and household alike.
// spent_in_budgets + not_in_budget == total_spending, the Insights spending
// figure for the month; unbudgeted breaks not_in_budget down by category
// (null = uncategorized) and sums to it.
export type BudgetMonthSummary = {
  month: string
  period_start: string
  period_end: string
  budget_count: number
  budgeted: string
  carryover: string
  available: string
  spent_in_budgets: string
  remaining: string
  not_in_budget: string
  total_spending: string
  unbudgeted: { category_id: number | null; spent: string }[]
}

export type CreateBudgetInput = {
  category_id: number
  period: BudgetPeriod
  amount: string
  rollover?: boolean
  rollover_start?: string
  is_active?: boolean
}

export type UpdateBudgetInput = {
  category_id?: number
  period?: BudgetPeriod
  amount?: string
  rollover?: boolean
  rollover_start?: string
  is_active?: boolean
}
