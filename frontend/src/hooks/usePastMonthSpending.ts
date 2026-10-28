import { useCallback, useEffect, useState } from 'react'
import { getCashFlow, getCategoryTrend, getSpendByCategory, getTopMerchants } from '../api/dashboard'
import {
  getHouseholdCashFlow,
  getHouseholdCategoryTrend,
  getHouseholdSpendByCategory,
  getHouseholdTopMerchants,
} from '../api/householdAggregator'
import { monthWindow } from '../lib/month'
import type { InsightsData } from './useScopedInsights'

export type PastMonthSpending =
  | { state: 'idle' | 'loading' | 'error'; retry: () => void }
  | {
      state: 'ready'
      byCategory: InsightsData['by_category']
      topMerchants: InsightsData['top_merchants']
      // The month's income / spending / net (#609); null if the server
      // returned no row for it.
      cashFlow: InsightsData['cash_flow'][number] | null
      // The income-vs-spending trend window ending at this month (#658).
      cashFlowSeries: InsightsData['cash_flow']
      // The month vs. the months before it (#611).
      categoryTrend: InsightsData['category_trend']
      retry: () => void
    }

// The trend shows six months, ending at the stepped-to month (#658) — the
// same window the page's own load uses for the current month.
const CASH_FLOW_MONTHS = 6

type Loaded =
  | {
      key: string
      ok: true
      byCategory: InsightsData['by_category']
      topMerchants: InsightsData['top_merchants']
      cashFlow: InsightsData['cash_flow'][number] | null
      cashFlowSeries: InsightsData['cash_flow']
      categoryTrend: InsightsData['category_trend']
    }
  | { key: string; ok: false }

// usePastMonthSpending loads one earlier month's category spend, top
// merchants (#607), income / spending totals (#609) and category trend
// against the months before it (#611); month null (the current month) loads
// nothing. Household scope reads the same figures from the aggregator (#619).
// Results are keyed by scope + month + attempt, so a slow response for a
// month the user has already stepped away from can't paint the wrong month.
export function usePastMonthSpending(month: string | null, scope: InsightsData['scope'] = 'personal'): PastMonthSpending {
  const [attempt, setAttempt] = useState(0)
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const retry = useCallback(() => setAttempt((n) => n + 1), [])
  const key = month == null ? null : `${scope}:${month}#${attempt}`

  useEffect(() => {
    if (month == null || key == null) return
    let cancelled = false
    const span = monthWindow(month)
    const load =
      scope === 'household'
        ? Promise.all([
            getHouseholdSpendByCategory(span),
            getHouseholdTopMerchants(10, span),
            getHouseholdCashFlow(CASH_FLOW_MONTHS, month),
            getHouseholdCategoryTrend(6, month),
          ])
        : Promise.all([getSpendByCategory(span), getTopMerchants(10, span), getCashFlow(CASH_FLOW_MONTHS, month), getCategoryTrend(6, month)])
    load
      .then(([byCategory, topMerchants, cashFlow, categoryTrend]) => {
        if (cancelled) return
        const row = cashFlow.find((m) => m.month.slice(0, 7) === month) ?? null
        setLoaded({ key, ok: true, byCategory, topMerchants, cashFlow: row, cashFlowSeries: cashFlow, categoryTrend })
      })
      .catch(() => {
        if (!cancelled) setLoaded({ key, ok: false })
      })
    return () => {
      cancelled = true
    }
  }, [month, scope, key])

  if (key == null) return { state: 'idle', retry }
  if (loaded?.key !== key) return { state: 'loading', retry }
  if (!loaded.ok) return { state: 'error', retry }
  return {
    state: 'ready',
    byCategory: loaded.byCategory,
    topMerchants: loaded.topMerchants,
    cashFlow: loaded.cashFlow,
    cashFlowSeries: loaded.cashFlowSeries,
    categoryTrend: loaded.categoryTrend,
    retry,
  }
}
