// Mirrors backend model.Budget + service.BudgetSpend.
export const BUDGET_PERIODS = ['monthly', 'weekly', 'annual'] as const
export type BudgetPeriod = (typeof BUDGET_PERIODS)[number]

export type Budget = {
  id: number
  user_id: number
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
