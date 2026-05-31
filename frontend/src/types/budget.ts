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
  spent: string
  remaining: string
  pct: number
}

export type CreateBudgetInput = {
  category_id: number
  period: BudgetPeriod
  amount: string
  rollover?: boolean
  is_active?: boolean
}

export type UpdateBudgetInput = {
  category_id?: number
  period?: BudgetPeriod
  amount?: string
  rollover?: boolean
  is_active?: boolean
}
