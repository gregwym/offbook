import { useCallback, useEffect, useState } from 'react'
import { getSpendByCategory, getTopMerchants } from '../api/dashboard'
import { monthWindow } from '../lib/month'
import type { InsightsData } from './useScopedInsights'

export type PastMonthSpending =
  | { state: 'idle' | 'loading' | 'error'; retry: () => void }
  | {
      state: 'ready'
      byCategory: InsightsData['by_category']
      topMerchants: InsightsData['top_merchants']
      retry: () => void
    }

type Loaded =
  | { key: string; ok: true; byCategory: InsightsData['by_category']; topMerchants: InsightsData['top_merchants'] }
  | { key: string; ok: false }

// usePastMonthSpending loads one earlier month's category spend and top
// merchants (#607); month null (the current month) loads nothing. Results
// are keyed by month + attempt, so a slow response for a month the user has
// already stepped away from can't paint the wrong month.
export function usePastMonthSpending(month: string | null): PastMonthSpending {
  const [attempt, setAttempt] = useState(0)
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const retry = useCallback(() => setAttempt((n) => n + 1), [])
  const key = month == null ? null : `${month}#${attempt}`

  useEffect(() => {
    if (month == null || key == null) return
    let cancelled = false
    const span = monthWindow(month)
    Promise.all([getSpendByCategory(span), getTopMerchants(10, span)])
      .then(([byCategory, topMerchants]) => {
        if (!cancelled) setLoaded({ key, ok: true, byCategory, topMerchants })
      })
      .catch(() => {
        if (!cancelled) setLoaded({ key, ok: false })
      })
    return () => {
      cancelled = true
    }
  }, [month, key])

  if (key == null) return { state: 'idle', retry }
  if (loaded?.key !== key) return { state: 'loading', retry }
  if (!loaded.ok) return { state: 'error', retry }
  return { state: 'ready', byCategory: loaded.byCategory, topMerchants: loaded.topMerchants, retry }
}
