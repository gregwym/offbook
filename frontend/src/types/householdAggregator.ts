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
  // false when a shared position had no price within the valuation stale
  // window — net_worth may rest on stale rates (#344).
  net_worth_complete: boolean
  income: string
  spending: string
  account_count: number
  transaction_count: number
  by_category: CategorySpendingItem[]
  live_member_count: number
  in_grace_count: number
  members: HouseholdMemberContribution[]
}

// AssetClassAllocation mirrors service/household.AssetClassAllocation.
// `kind` is the position's asset kind (e.g. "cash", "equity", "crypto").
// `complete` is false when a position of this kind had no available price, so
// `value` is a partial sum (#282).
export type HouseholdAssetClassAllocation = {
  kind: string
  value: string
  complete: boolean
}

// NetWorthPoint mirrors service/household.NetWorthPoint. Date arrives as
// RFC3339 (e.g. "2024-01-15T00:00:00Z"); the UI slices for month buckets.
// `complete` is false when an asset held at that month-end had no price.
export type HouseholdNetWorthPoint = {
  date: string
  value: string
  complete: boolean
}

// AccountSummary mirrors service/household.AccountSummary — lightweight,
// non-PII account projection used for the Insights account list band.
// `complete` is false when a position in the account had no price observed
// within the valuation stale window, so `balance` may rest on a stale rate
// (#339).
export type HouseholdAccountSummary = {
  account_id: number
  name: string
  account_type: string
  currency: string
  balance: string
  owner_user_id: number
  visibility: 'balance_only' | 'balance_and_txns'
  complete: boolean
}
