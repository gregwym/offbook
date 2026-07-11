// Regression coverage for the #266 bug class: one failing data source inside
// the Insights fan-out must not blank the whole page. The original #266/#267
// fix protected a since-removed `getPortfolioSummary()` call with its own
// `Promise.allSettled`; that call site is gone post-M10 (the allocation band
// now reads `getAllocation()` directly), but the same resilience pattern
// lives on for per-budget spend (`loadPersonal`'s `Promise.allSettled` over
// `getBudgetSpend`). The "partial success" case below exercises that path:
// one budget's spend call fails, the rest of the page must still render.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { renderHook, waitFor } from '@testing-library/react'
import { server } from '../test/server'
import { resetStores } from '../test/testUtils'
import { useScopeStore } from '../store/scopeStore'
import { useScopedInsights } from './useScopedInsights'
import * as fx from '../test/fixtures'

const API = '/api/v1'

describe('useScopedInsights (personal scope)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('stays in loading state until scope hydrates', () => {
    useScopeStore.setState({ hydrated: false })
    const { result } = renderHook(() => useScopedInsights())
    expect(result.current.state).toBe('loading')
  })

  it('surfaces an error state on a network failure', async () => {
    server.use(http.get(`${API}/dashboard/summary`, () => HttpResponse.error()))
    const { result } = renderHook(() => useScopedInsights())
    await waitFor(() => expect(result.current.state).toBe('error'))
  })

  it('surfaces an error state with the backend message on a 404', async () => {
    server.use(
      http.get(`${API}/dashboard/net-worth`, () =>
        HttpResponse.json({ error: 'not found', code: 'NOT_FOUND' }, { status: 404 }),
      ),
    )
    const { result } = renderHook(() => useScopedInsights())
    await waitFor(() => expect(result.current.state).toBe('error'))
    if (result.current.state === 'error') {
      expect(result.current.error).toBe('not found')
    }
  })

  it('drops a single failing budget row instead of failing the whole page (partial success)', async () => {
    server.use(http.get(`${API}/budgets/:id/spend`, () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    const { result } = renderHook(() => useScopedInsights())
    await waitFor(() => expect(result.current.state).toBe('ready'))
    if (result.current.state !== 'ready') throw new Error('unreachable')
    // The page still surfaces the other four bands — this is the assertion
    // that would have caught #266: one broken fan-out call degrades its own
    // band, it doesn't blank the page.
    expect(result.current.data.net_worth).toBe(fx.dashboardSummary.net_worth)
    expect(result.current.data.budgets).toEqual([])
  })

  it('returns the full computed shape on full success', async () => {
    const { result } = renderHook(() => useScopedInsights())
    await waitFor(() => expect(result.current.state).toBe('ready'))
    if (result.current.state !== 'ready') throw new Error('unreachable')
    const { data } = result.current
    expect(data.scope).toBe('personal')
    expect(data.net_worth).toBe(fx.dashboardSummary.net_worth)
    expect(data.income).toBe(fx.dashboardSummary.income)
    expect(data.spending).toBe(fx.dashboardSummary.spending)
    expect(data.by_category).toEqual(fx.dashboardSummary.by_category)
    expect(data.net_worth_trend).toEqual([
      { date: fx.netWorthTrend[0].date, value: fx.netWorthTrend[0].total, complete: true },
    ])
    expect(data.allocation).toEqual([{ kind: 'cash', value: fx.allocation[0].value, complete: true }])
    expect(data.budgets).toEqual([
      {
        id: fx.budget1.id,
        category_id: fx.budget1.category_id,
        category_name: fx.categoryGroceries.name,
        period: fx.budget1.period,
        limit: fx.budgetSpend1.limit,
        spent: fx.budgetSpend1.spent,
        pct: fx.budgetSpend1.pct,
      },
    ])
    expect(data.goals).toEqual([
      {
        id: fx.goal1.id,
        name: fx.goal1.name,
        target: fx.goal1.target_amount,
        current: fx.goal1.current_amount,
        progress_pct: fx.goal1.progress_pct,
        target_date: null,
      },
    ])
    expect(data.accounts).toEqual([
      {
        id: fx.account1.id,
        name: fx.account1.name,
        account_type: fx.account1.account_type,
        currency: fx.account1.currency,
        balance: fx.account1.balance,
        balance_complete: true,
        source: 'manual',
        last_synced_at: null,
      },
    ])
  })

  afterEach(() => {
    server.resetHandlers()
  })
})
