// useScopedInsights — single hook backing the InsightsPage 5 bands.
// Picks personal vs household data sources via the active scope and
// returns a normalized shape so the InsightsPage doesn't branch on scope.
//
// Household scope fans out to the aggregator (`service/household`). The
// aggregator is the only path for cross-user reads; this hook must never
// call household repos directly. Personal scope hits the existing per-user
// dashboard/portfolio/budget/goal/account endpoints.
import { useEffect, useState } from 'react'
import { listAccounts } from '../api/accounts'
import { getBudgetSpend, listBudgets } from '../api/budgets'
import { listCategories } from '../api/categories'
import { getDashboardSummary, getNetWorth } from '../api/dashboard'
import {
  getBudgetPace,
  getGoalProgress,
  getHouseholdAccountSummaries,
  getHouseholdAllocation,
  getHouseholdDashboard,
  getHouseholdNetWorthTrend,
} from '../api/householdAggregator'
import { listGoals } from '../api/savingsGoals'
import { useScopeStore } from '../store/scopeStore'
import { SCOPE_HOUSEHOLD } from '../types/scope'

export type InsightsCategoryRow = {
  category_id: number | null
  name: string
  amount: string
}

export type InsightsTrendPoint = {
  date: string // YYYY-MM-DD (we slice off any time component)
  value: string
}

export type InsightsAllocationRow = {
  kind: string
  value: string
  // weight_pct present for personal portfolio (server-computed), absent
  // for household (would require a sum-then-divide that we don't bother
  // with here — UI can compute if needed).
  weight_pct?: string
}

export type InsightsBudgetRow = {
  id: number
  category_id: number
  category_name: string
  period: string
  limit: string
  spent: string
  pct: number // 0..N (>1.0 = over budget)
}

export type InsightsGoalRow = {
  id: number
  name: string
  target: string
  current: string
  progress_pct: number // 0..1
  target_date: string | null
}

export type InsightsAccountRow = {
  id: number
  name: string
  account_type: string
  currency: string
  balance: string
  source: 'plaid' | 'manual'
  last_synced_at: string | null
  owner_user_id?: number
  visibility?: 'balance_only' | 'balance_and_txns'
}

export type InsightsData = {
  scope: 'personal' | 'household'
  period: { from: string; to: string }
  net_worth: string
  income: string
  spending: string
  by_category: InsightsCategoryRow[]
  net_worth_trend: InsightsTrendPoint[]
  allocation: InsightsAllocationRow[]
  budgets: InsightsBudgetRow[]
  goals: InsightsGoalRow[]
  accounts: InsightsAccountRow[]
  // Household-only counts. Surfaced so the page can render the
  // "live / in-grace" hint without re-fetching.
  live_member_count?: number
  in_grace_count?: number
}

type Result =
  | { state: 'idle' }
  | { state: 'loading' }
  | { state: 'error'; error: string }
  | { state: 'ready'; data: InsightsData }

export function useScopedInsights(): Result {
  const { active, hydrated, householdId } = useScopeStore()
  const [result, setResult] = useState<Result>({ state: 'loading' })

  useEffect(() => {
    if (!hydrated) return
    let cancelled = false

    const run = async () => {
      try {
        const data =
          active === SCOPE_HOUSEHOLD && householdId != null
            ? await loadHousehold()
            : await loadPersonal()
        if (!cancelled) setResult({ state: 'ready', data })
      } catch (err) {
        if (!cancelled) setResult({ state: 'error', error: errMsg(err) })
      }
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [active, hydrated, householdId])

  return result
}

async function loadPersonal(): Promise<InsightsData> {
  // Allocation band has no data source in personal scope right now —
  // the legacy /investments/portfolio endpoint was removed per ADR-0013
  // and its position-based replacement lands in M10b #238. Until then
  // the band renders its empty state ("No investments yet…") via the
  // empty allocation array below. Household scope is unaffected — it
  // goes through the aggregator's /h/insights/allocation route.
  const [summary, trend, budgets, goals, accountsResp, categories] =
    await Promise.all([
      getDashboardSummary('current_month'),
      getNetWorth(12),
      listBudgets(),
      listGoals(),
      listAccounts({}),
      listCategories(),
    ])

  // Per-budget spend — fan out, drop silently on row failures so one
  // missing spend doesn't blank the band.
  const activeBudgets = budgets.filter((b) => b.is_active)
  const spendResults = await Promise.allSettled(
    activeBudgets.map((b) => getBudgetSpend(b.id)),
  )

  const categoryName = new Map<number, string>()
  for (const c of categories) categoryName.set(c.id, c.name)

  const budgetRows: InsightsBudgetRow[] = []
  activeBudgets.forEach((b, i) => {
    const r = spendResults[i]
    if (r.status !== 'fulfilled') return
    budgetRows.push({
      id: b.id,
      category_id: b.category_id,
      category_name: categoryName.get(b.category_id) ?? '—',
      period: b.period,
      limit: r.value.limit,
      spent: r.value.spent,
      pct: r.value.pct,
    })
  })

  return {
    scope: 'personal',
    period: { from: summary.period.from, to: summary.period.to },
    net_worth: summary.net_worth,
    income: summary.income,
    spending: summary.spending,
    by_category: summary.by_category,
    net_worth_trend: trend.map((p) => ({ date: p.date.slice(0, 10), value: p.total })),
    allocation: [], // see header comment — restored when M10b #238 lands

    budgets: budgetRows,
    goals: goals.map((g) => ({
      id: g.id,
      name: g.name,
      target: g.target_amount,
      current: g.current_amount,
      progress_pct: g.progress_pct,
      target_date: g.target_date ?? null,
    })),
    accounts: accountsResp.items.map((a) => ({
      id: a.id,
      name: a.name,
      account_type: a.account_type,
      currency: a.currency,
      balance: a.balance,
      source: a.plaid_account_id ? 'plaid' : 'manual',
      last_synced_at: a.last_synced_at,
    })),
  }
}

async function loadHousehold(): Promise<InsightsData> {
  const [dashboard, trend, allocation, accounts, budgetPace, goalProgress] =
    await Promise.all([
      getHouseholdDashboard('current_month'),
      getHouseholdNetWorthTrend(12),
      getHouseholdAllocation(),
      getHouseholdAccountSummaries(),
      getBudgetPace('current_month'),
      getGoalProgress(),
    ])

  // BudgetPace lacks category names — surface category_id, the page
  // shows "Category #N" if we can't resolve. (The household budget
  // CRUD UI is still backlogged so a category lookup endpoint at the
  // household level isn't worth wiring just for this band.)
  const budgetRows: InsightsBudgetRow[] = budgetPace.map((p) => ({
    id: p.budget_id,
    category_id: p.category_id,
    category_name: `Category #${p.category_id}`,
    period: p.period,
    limit: p.budget,
    spent: p.spent,
    pct: Number.parseFloat(p.pace) || 0,
  }))

  return {
    scope: 'household',
    period: { from: dashboard.period.from, to: dashboard.period.to },
    net_worth: dashboard.net_worth,
    income: dashboard.income,
    spending: dashboard.spending,
    by_category: dashboard.by_category,
    net_worth_trend: trend.map((p) => ({ date: p.date.slice(0, 10), value: p.value })),
    allocation: allocation.map((a) => ({ kind: a.kind, value: a.value })),
    budgets: budgetRows,
    goals: goalProgress.map((g) => ({
      id: g.goal_id,
      name: g.name,
      target: g.target_amount,
      current: g.current_amount,
      progress_pct: Number.parseFloat(g.progress) || 0,
      target_date: g.target_date ?? null,
    })),
    accounts: accounts.map((a) => ({
      id: a.account_id,
      name: a.name,
      account_type: a.account_type,
      currency: a.currency,
      balance: a.balance,
      // Household-shared accounts can be either Plaid-linked or manual,
      // but the aggregator doesn't surface that distinction (would leak
      // a per-owner plaid_item indicator). Default to "manual" badge.
      source: 'manual',
      last_synced_at: null,
      owner_user_id: a.owner_user_id,
      visibility: a.visibility,
    })),
    live_member_count: dashboard.live_member_count,
    in_grace_count: dashboard.in_grace_count,
  }
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
