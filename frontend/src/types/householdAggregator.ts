// Mirror of backend service/household/aggregator types.

export type HouseholdPeriodKey = 'current_month' | 'last_30d' | 'ytd'

export type HouseholdPeriod = {
  key: HouseholdPeriodKey
  from: string
  to: string
}

export type CategorySpendingItem = {
  category_id: number | null
  name: string
  amount: string // decimal string (NUMERIC(30,18))
}

export type HouseholdMemberContribution = {
  user_id: number
  role: 'owner' | 'contributor' | 'view_only'
  net_worth_contribution: string
  spending_contribution: string
  account_count: number
}

// BudgetPaceItem mirrors service/household.BudgetPaceItem.
export type BudgetPaceItem = {
  budget_id: number
  category_id: number
  period: 'monthly' | 'weekly' | 'annual'
  budget: string
  spent: string
  pace: string // ratio 0..N (1.0 = on budget)
}

// GoalProgressItem mirrors service/household.GoalProgressItem.
export type GoalProgressItem = {
  goal_id: number
  name: string
  target_amount: string
  current_amount: string
  progress: string // ratio 0..1
  target_date?: string | null
}

export type HouseholdDashboard = {
  period: HouseholdPeriod
  net_worth: string
  income: string
  spending: string
  account_count: number
  transaction_count: number
  by_category: CategorySpendingItem[]
  live_member_count: number
  in_grace_count: number
  members: HouseholdMemberContribution[]
}
