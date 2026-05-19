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
