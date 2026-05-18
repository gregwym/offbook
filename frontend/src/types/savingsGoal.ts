// Mirrors backend/internal/service.GoalView — the persisted SavingsGoal
// plus computed progress + remaining (both server-side via service.View).
export type SavingsGoal = {
  id: number
  user_id: number
  name: string
  target_amount: string
  current_amount: string
  target_date?: string | null
  account_id?: number | null
  created_at: string
  updated_at: string
  progress_pct: number
  remaining: string
}

export type CreateGoalInput = {
  name: string
  target_amount: string
  target_date?: string | null
  account_id?: number | null
}

export type UpdateGoalInput = {
  name?: string
  target_amount?: string
  target_date?: string | null
  clear_target_date?: boolean
  account_id?: number | null
  clear_account_id?: boolean
}

export type ContributionInput = {
  amount: string
}
