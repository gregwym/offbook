// Shared MSW fixture data — realistic-shaped responses for every endpoint
// the pages/hooks under test call. Kept in one place so a page test and the
// hook test agree on what "the API returned something sane" looks like.
import type { Account } from '../types/account'
import type { Asset } from '../types/asset'
import type { Budget, BudgetSpend } from '../types/budget'
import type { Category } from '../types/category'
import type { CategorizationRule } from '../types/categorizationRule'
import type { AssetClassAllocation, DashboardSummary, NetWorthPoint } from '../types/dashboard'
import type {
  HouseholdAccountSummary,
  HouseholdAssetClassAllocation,
  HouseholdDashboard,
  HouseholdNetWorthPoint,
} from '../types/householdAggregator'
import type { HouseholdDetail, MembersListing } from '../types/household'
import type { PlaidItem } from '../types/plaid'
import type { SavingsGoal } from '../types/savingsGoal'
import type { UserSettingsView } from '../types/userSettings'
import type { Transaction } from '../types/transaction'

const now = '2026-07-01T00:00:00Z'

export const categoryGroceries: Category = {
  id: 1,
  parent_id: null,
  name: 'Groceries',
  slug: 'groceries',
  icon: null,
  color: null,
  is_system: true,
  created_at: now,
  updated_at: now,
}

export const categoryIncome: Category = {
  id: 2,
  parent_id: null,
  name: 'Income',
  slug: 'income',
  icon: null,
  color: null,
  is_system: true,
  created_at: now,
  updated_at: now,
}

export const categories: Category[] = [categoryGroceries, categoryIncome]

export const account1: Account = {
  id: 1,
  user_id: 1,
  name: 'Checking',
  institution_slug: 'manual',
  account_type: 'checking',
  currency: 'USD',
  primary_quote_asset_id: 1,
  balance: '1234.560000000000000000',
  balance_complete: true,
  last_four: '1234',
  plaid_account_id: null,
  plaid_item_id: null,
  is_active: true,
  created_at: now,
  updated_at: now,
  last_sync_status: null,
  last_synced_at: null,
  last_sync_error: null,
}

export const accounts: Account[] = [account1]

export const budget1: Budget = {
  id: 1,
  user_id: 1,
  household_id: null,
  category_id: 1,
  period: 'monthly',
  amount: '500.000000000000000000',
  rollover: false,
  is_active: true,
  created_at: now,
  updated_at: now,
}

export const budgets: Budget[] = [budget1]

export const budgetSpend1: BudgetSpend = {
  budget_id: 1,
  category_id: 1,
  period: 'monthly',
  period_start: '2026-07-01',
  period_end: '2026-07-31',
  limit: '500.000000000000000000',
  spent: '120.000000000000000000',
  remaining: '380.000000000000000000',
  pct: 0.24,
}

export const goal1: SavingsGoal = {
  id: 1,
  user_id: 1,
  household_id: null,
  name: 'Emergency fund',
  target_amount: '1000.000000000000000000',
  current_amount: '200.000000000000000000',
  target_date: null,
  account_id: null,
  created_at: now,
  updated_at: now,
  progress_pct: 0.2,
  remaining: '800.000000000000000000',
}

export const goals: SavingsGoal[] = [goal1]

export const transaction1: Transaction = {
  id: 1,
  user_id: 1,
  account_id: 1,
  asset_id: 1,
  category_id: 1,
  amount: '-50.000000000000000000',
  description: 'WHOLEFDS MKT',
  description_clean: 'Whole Foods',
  merchant_name: 'Whole Foods',
  transaction_date: '2026-07-01',
  posted_date: '2026-07-01',
  source: 'manual',
  external_id: null,
  plaid_transaction_id: null,
  categorization_method: 'manual',
  is_transfer: false,
  transfer_pair_id: null,
  notes: null,
  created_at: now,
  updated_at: now,
}

export const transactions: Transaction[] = [transaction1]

export const rule1: CategorizationRule = {
  id: 1,
  user_id: 1,
  pattern: 'WHOLEFDS',
  category_id: 1,
  match_type: 'contains',
  priority: 0,
  is_active: true,
  created_at: now,
  updated_at: now,
}

export const rules: CategorizationRule[] = [rule1]

export const assetUSD: Asset = {
  id: 1,
  symbol: 'USD',
  kind: 'fiat',
  display_name: 'US Dollar',
  quote_currency_asset_id: null,
  precision: 2,
  created_at: now,
  updated_at: now,
}

export const assets: Asset[] = [assetUSD]

export const dashboardSummary: DashboardSummary = {
  period: { key: 'current_month', from: '2026-07-01', to: '2026-07-31' },
  net_worth: '10000.000000000000000000',
  net_worth_complete: true,
  income: '5000.000000000000000000',
  spending: '3000.000000000000000000',
  account_count: 1,
  transaction_count: 1,
  by_category: [{ category_id: 1, name: 'Groceries', amount: '120.000000000000000000' }],
}

export const netWorthTrend: NetWorthPoint[] = [
  { date: '2026-06-30', total: '9000.000000000000000000', complete: true },
]

export const allocation: AssetClassAllocation[] = [
  { kind: 'cash', value: '10000.000000000000000000', complete: true },
]

export const userSettings: UserSettingsView = {
  user_id: 1,
  preferred_provider: 'claude',
  api_endpoint: null,
  api_token_set: false,
  preferred_model: null,
  auto_price_refresh: false,
}

export const plaidItems: PlaidItem[] = []

// ── Household fixtures (household-scope tests only) ──

export const householdDetail: HouseholdDetail = {
  household: { id: 10, name: 'The Test House', grace_period_days: 30, created_at: now, updated_at: now },
  members: [{ id: 1, household_id: 10, user_id: 1, role: 'owner', joined_at: now, left_at: null, purged_at: null }],
  role: 'owner',
}

export const membersListing: MembersListing = {
  active: householdDetail.members,
  in_grace: [],
}

export const householdDashboard: HouseholdDashboard = {
  period: { key: 'current_month', from: '2026-07-01', to: '2026-07-31' },
  net_worth: '20000.000000000000000000',
  net_worth_complete: true,
  income: '8000.000000000000000000',
  spending: '4000.000000000000000000',
  account_count: 2,
  transaction_count: 5,
  by_category: [],
  live_member_count: 1,
  in_grace_count: 0,
  members: [],
}

export const householdAllocation: HouseholdAssetClassAllocation[] = []
export const householdNetWorthTrend: HouseholdNetWorthPoint[] = []
export const householdAccountSummaries: HouseholdAccountSummary[] = []
