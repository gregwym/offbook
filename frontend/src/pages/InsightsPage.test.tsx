import { beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores, setHouseholdScope } from '../test/testUtils'
import * as fx from '../test/fixtures'
import { InsightsPage } from './InsightsPage'
import { currentMonth, monthLabel, monthWindow, shiftMonth } from '../lib/month'
import { dayBefore } from '../lib/spendingLink'

describe('InsightsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the 5 personal-scope bands without a stuck loading state or error banner', async () => {
    renderPage(<InsightsPage />)
    await expectHealthySmoke()
  })
})

describe('InsightsPage first run', () => {
  beforeEach(() => {
    resetStores()
  })

  // #520: a brand-new user sees one call to action, not price controls for
  // assets they don't have yet.
  it('shows only the add-account call to action when there are no accounts', async () => {
    server.use(http.get('/api/v1/accounts', () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<InsightsPage />)
    expect(await screen.findByRole('heading', { name: 'Add your first account' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: /refresh prices/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /set price/i })).toBeNull()
  })
})

describe('InsightsPage net-worth range selector', () => {
  beforeEach(() => {
    resetStores()
  })

  it('refetches only the trend with the chosen months and marks it pressed', async () => {
    const requested: string[] = []
    server.use(
      http.get('*/dashboard/net-worth', ({ request }) => {
        requested.push(new URL(request.url).searchParams.get('months') ?? '')
        return HttpResponse.json({ data: [], total: 0 })
      }),
    )
    renderPage(<InsightsPage />)
    const group = await screen.findByRole('group', { name: 'Net worth date range' })
    expect(screen.getByRole('button', { name: '1Y' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(screen.getByRole('button', { name: '3M' }))
    await waitFor(() => expect(requested).toContain('3'))
    expect(screen.getByRole('button', { name: '3M' })).toHaveAttribute('aria-pressed', 'true')
    expect(group).toBeInTheDocument()
  })

  it('keeps the previous series and shows an error when the range fetch fails', async () => {
    renderPage(<InsightsPage />)
    await screen.findByRole('group', { name: 'Net worth date range' })
    server.use(
      http.get('*/dashboard/net-worth', () =>
        HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 }),
      ),
    )
    fireEvent.click(screen.getByRole('button', { name: '6M' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(/Couldn.t load that range/)
    expect(screen.getByRole('button', { name: '1Y' })).toHaveAttribute('aria-pressed', 'true')
  })
})

describe('InsightsPage net-worth asset-class drill-in', () => {
  beforeEach(() => {
    resetStores()
  })

  it('shows start vs latest value per asset class when by_kind is present', async () => {
    server.use(
      http.get('*/dashboard/net-worth', () =>
        HttpResponse.json({
          data: [
            { date: '2026-03-31', total: '1000', complete: true, by_kind: [{ kind: 'fiat', value: '1000', complete: true }] },
            {
              date: '2026-05-31',
              total: '1650',
              complete: true,
              by_kind: [
                { kind: 'equity', value: '150', complete: true },
                { kind: 'fiat', value: '1500', complete: false },
              ],
            },
          ],
          total: 2,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    fireEvent.click(await screen.findByText('By asset class'))
    const row = screen.getByRole('rowheader', { name: 'Stocks' }).closest('tr') as HTMLElement
    expect(row).toHaveTextContent('150.00')
    expect(screen.getByRole('rowheader', { name: /^Cash/ }).closest('tr')).toHaveTextContent('500.00')
    // #524: storage codes never reach the user.
    expect(screen.queryByRole('rowheader', { name: /fiat|equity/i })).toBeNull()
  })
})

describe('InsightsPage allocation labels', () => {
  beforeEach(() => {
    resetStores()
  })

  // #524: the allocation band names asset classes in plain language.
  it('labels asset classes Cash / Stocks / Crypto instead of backend codes', async () => {
    server.use(
      http.get('*/dashboard/allocation', () =>
        HttpResponse.json({
          data: [
            { kind: 'fiat', value: '600', complete: true },
            { kind: 'equity', value: '300', complete: true },
            { kind: 'crypto', value: '100', complete: true },
          ],
          total: 3,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    const band = (await screen.findByRole('heading', { name: /asset allocation/i })).closest('section') as HTMLElement
    await waitFor(() => expect(within(band).getByText('Cash')).toBeTruthy())
    expect(within(band).getByText('Stocks')).toBeTruthy()
    expect(within(band).getByText('Crypto')).toBeTruthy()
    expect(band).toHaveTextContent('60.0%')
    expect(band).not.toHaveTextContent(/fiat|equity/i)
  })
})

describe('InsightsPage household asset-class drill-in', () => {
  beforeEach(() => {
    resetStores()
  })

  it('shows the by-asset-class breakdown in household scope from the aggregator', async () => {
    setHouseholdScope(1)
    server.use(
      http.get('*/h/insights/net-worth', () =>
        HttpResponse.json({
          data: [
            { date: '2026-03-31T00:00:00Z', value: '1000', complete: true, by_kind: [{ kind: 'fiat', value: '1000', complete: true }] },
            {
              date: '2026-05-31T00:00:00Z',
              value: '1650',
              complete: true,
              by_kind: [
                { kind: 'equity', value: '150', complete: true },
                { kind: 'fiat', value: '1500', complete: true },
              ],
            },
          ],
          total: 2,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    fireEvent.click(await screen.findByText('By asset class'))
    expect(screen.getByRole('rowheader', { name: 'Stocks' }).closest('tr')).toHaveTextContent('150.00')
  })
})

describe('InsightsPage spending band refunds', () => {
  beforeEach(() => {
    resetStores()
  })

  it('labels a net-negative category as a net refund without drawing a bar', async () => {
    server.use(
      http.get('*/dashboard/spend-by-category', () =>
        HttpResponse.json({
          data: [
            { category_id: 1, name: 'Groceries', amount: '120.000000000000000000' },
            { category_id: 2, name: 'Shopping', amount: '-30.000000000000000000' },
          ],
          total: 2,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    const row = (await screen.findByText('Shopping')).closest('li') as HTMLElement
    expect(row).toHaveTextContent('net refund')
    const bar = row.querySelector('.bg-indigo-400') as HTMLElement
    expect(bar.style.width).toBe('0%')
  })

  it('shows ordinary spending as positive with a bar, ignoring the signed summary (#456)', async () => {
    server.use(
      http.get('*/dashboard/summary', () =>
        HttpResponse.json({
          data: {
            ...fx.dashboardSummary,
            by_category: [
              { category_id: 1, name: 'Groceries', amount: '-130.000000000000000000' },
              { category_id: 3, name: 'Income', amount: '3200.000000000000000000' },
            ],
          },
        }),
      ),
      http.get('*/dashboard/spend-by-category', () =>
        HttpResponse.json({
          data: [{ category_id: 1, name: 'Groceries', amount: '130.000000000000000000' }],
          total: 1,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    const band = (await screen.findByText(/spending by category/i)).closest('section') as HTMLElement
    const row = within(band).getAllByText('Groceries')[0].closest('li') as HTMLElement
    expect(row).toHaveTextContent('130.00')
    expect(row).not.toHaveTextContent('net refund')
    expect((row.querySelector('.bg-indigo-400') as HTMLElement).style.width).toBe('100%')
    expect(within(band).queryByText('Income')).toBeNull()
  })
})

// #571: personal spending figures open the transactions behind them;
// household aggregates have no member transaction list, so no links.
describe('InsightsPage spending drill-down links', () => {
  beforeEach(() => {
    resetStores()
  })

  it('links categories, merchants and budgets to the spending lens for the period', async () => {
    server.use(
      http.get('*/dashboard/summary', () =>
        HttpResponse.json({
          data: {
            ...fx.dashboardSummary,
            period: { key: 'current_month', from: '2026-07-01T00:00:00Z', to: '2026-08-01T00:00:00Z' },
          },
        }),
      ),
    )
    renderPage(<InsightsPage />)
    const band = (await screen.findByText(/spending by category/i)).closest('section') as HTMLElement
    expect(within(band).getByRole('link', { name: 'Groceries' })).toHaveAttribute(
      'href',
      '/transactions?spending=1&category_id=1&from=2026-07-01&to=2026-07-31',
    )
    expect(within(band).getByRole('link', { name: 'Whole Foods' })).toHaveAttribute(
      'href',
      '/transactions?spending=1&merchant=Whole+Foods&from=2026-07-01&to=2026-07-31',
    )
    const budgets = (await screen.findByText(/budgets · this period/i)).closest('div.rounded-lg') as HTMLElement
    await waitFor(() => expect(within(budgets).getAllByRole('link').some((a) => a.getAttribute('href')?.startsWith('/transactions?spending=1&category_id=1'))).toBe(true))
  })

  it('renders household spending without drill-down links', async () => {
    setHouseholdScope(1)
    renderPage(<InsightsPage />)
    const band = (await screen.findByText(/spending by category/i)).closest('section') as HTMLElement
    await waitFor(() => expect(within(band).queryAllByRole('link')).toHaveLength(0))
  })
})

describe('InsightsPage net-worth missing exchange rate', () => {
  beforeEach(() => {
    resetStores()
  })

  // #599: a partial headline names the currency with no rate and lets the
  // user enter it, instead of a Set price that can't pick a currency.
  it('names each unpriced asset and sets an exchange rate for a currency', async () => {
    const eur = { ...fx.assetUSD, id: 7, symbol: 'EUR', display_name: 'Euro' }
    let posted: { assetID: string; body: unknown } | null = null
    server.use(
      http.get('/api/v1/dashboard/summary', () =>
        HttpResponse.json({
          data: {
            ...fx.dashboardSummary,
            net_worth_complete: false,
            net_worth_currency: 'USD',
            net_worth_unpriced: [
              { asset_id: 7, symbol: 'EUR', kind: 'fiat' },
              { asset_id: 9, symbol: 'MYFUND', kind: 'fund' },
            ],
          },
        }),
      ),
      http.get('/api/v1/assets', () => HttpResponse.json({ data: [fx.assetUSD, eur], total: 2 })),
      http.post('/api/v1/assets/:id/prices', async ({ params, request }) => {
        posted = { assetID: String(params.id), body: await request.json() }
        return HttpResponse.json({ data: { id: 1 } }, { status: 201 })
      }),
    )
    renderPage(<InsightsPage />)

    expect(await screen.findByText('EUR → USD')).toBeTruthy()
    expect(screen.getByText(/Needs an exchange rate:/)).toBeTruthy()
    expect(screen.getByText('MYFUND')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Set price for MYFUND' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Set exchange rate for EUR' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Set exchange rate for EUR')).toBeTruthy()
    expect(within(dialog).queryByText('Symbol')).toBeNull()
    fireEvent.change(within(dialog).getByLabelText('1 EUR in USD'), { target: { value: '1.08' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Set rate' }))

    await waitFor(() => expect(posted).not.toBeNull())
    expect(posted!.assetID).toBe('7')
    expect(posted!.body).toMatchObject({ price: '1.08' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  // #601: the household headline names the missing rate in the owning
  // member's currency, and Set rate writes exactly that pair.
  it('names a household unpriced currency and sets the rate into its quote', async () => {
    setHouseholdScope(1)
    const eur = { ...fx.assetUSD, id: 7, symbol: 'EUR', display_name: 'Euro' }
    let posted: { assetID: string; body: unknown } | null = null
    server.use(
      http.get('/api/v1/h/dashboard', () =>
        HttpResponse.json({
          data: {
            ...fx.householdDashboard,
            net_worth_complete: false,
            net_worth_unpriced: [{ asset_id: 7, symbol: 'EUR', kind: 'fiat', quote: 'CAD', quote_asset_id: 4 }],
          },
        }),
      ),
      http.get('/api/v1/assets', () => HttpResponse.json({ data: [fx.assetUSD, eur], total: 2 })),
      http.post('/api/v1/assets/:id/prices', async ({ params, request }) => {
        posted = { assetID: String(params.id), body: await request.json() }
        return HttpResponse.json({ data: { id: 1 } }, { status: 201 })
      }),
    )
    renderPage(<InsightsPage />)

    expect(await screen.findByText('EUR → CAD')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Set exchange rate for EUR' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('1 EUR in CAD'), { target: { value: '1.5' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Set rate' }))

    await waitFor(() => expect(posted).not.toBeNull())
    expect(posted!.assetID).toBe('7')
    expect(posted!.body).toMatchObject({ price: '1.5', quote_asset_id: 4 })
  })
})

// #607: personal spending steps back a month for the month-end review.
describe('InsightsPage spending month stepper', () => {
  beforeEach(() => {
    resetStores()
  })

  const prev = shiftMonth(currentMonth(), -1)
  const prevWindow = monthWindow(prev)

  it('loads the earlier month for categories and merchants and drills into that month', async () => {
    const requested: { path: string; from: string | null; to: string | null }[] = []
    const record = (request: Request) => {
      const u = new URL(request.url)
      requested.push({ path: u.pathname, from: u.searchParams.get('from'), to: u.searchParams.get('to') })
    }
    server.use(
      http.get('*/dashboard/spend-by-category', ({ request }) => {
        record(request)
        const past = new URL(request.url).searchParams.get('from') != null
        return HttpResponse.json({
          data: [{ category_id: 7, name: past ? 'Travel' : 'Groceries', amount: past ? '410.250000000000000000' : '120.000000000000000000' }],
          total: 1,
        })
      }),
      http.get('*/dashboard/top-merchants', ({ request }) => {
        record(request)
        const past = new URL(request.url).searchParams.get('from') != null
        return HttpResponse.json({
          data: [{ merchant: past ? 'Air Canada' : 'Whole Foods', amount: '410.250000000000000000', count: 1 }],
          total: 1,
        })
      }),
    )
    renderPage(<InsightsPage />)
    const nav = await screen.findByRole('navigation', { name: 'Spending month' })
    expect(within(nav).getByRole('button', { name: 'Next month' })).toBeDisabled()
    const band = nav.closest('section') as HTMLElement
    await within(band).findByRole('link', { name: 'Groceries' })

    fireEvent.click(within(nav).getByRole('button', { name: 'Previous month' }))
    const travel = await screen.findByRole('link', { name: 'Travel' })
    expect(screen.getByRole('heading', { name: `Spending by category · ${monthLabel(prev)}` })).toBeInTheDocument()
    expect(screen.getByText(`Top merchants · ${monthLabel(prev)}`)).toBeInTheDocument()
    // The trend list stays on the current month and says so.
    expect(screen.getByText(`${monthLabel(currentMonth())} vs. average`)).toBeInTheDocument()
    expect(requested).toContainEqual({ path: '/api/v1/dashboard/spend-by-category', ...prevWindow })
    expect(requested).toContainEqual({ path: '/api/v1/dashboard/top-merchants', ...prevWindow })

    const to = dayBefore(prevWindow.to)
    expect(travel.getAttribute('href')).toBe(`/transactions?spending=1&category_id=7&from=${prevWindow.from}&to=${to}`)
    expect(screen.getByRole('link', { name: 'Air Canada' }).getAttribute('href')).toBe(
      `/transactions?spending=1&merchant=Air+Canada&from=${prevWindow.from}&to=${to}`,
    )

    fireEvent.click(within(nav).getByRole('button', { name: 'This month' }))
    expect(await within(band).findByRole('link', { name: 'Groceries' })).toBeInTheDocument()
    expect(within(nav).getByRole('button', { name: 'Next month' })).toBeDisabled()
  })

  it('shows an inline error with Retry when the earlier month fails, keeping the page healthy', async () => {
    let fail = true
    server.use(
      http.get('*/dashboard/spend-by-category', ({ request }) => {
        if (new URL(request.url).searchParams.get('from') == null) {
          return HttpResponse.json({ data: fx.spendByCategory, total: 1 })
        }
        if (fail) return HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 })
        return HttpResponse.json({ data: [{ category_id: 9, name: 'Dining', amount: '55.000000000000000000' }], total: 1 })
      }),
    )
    renderPage(<InsightsPage />)
    const nav = await screen.findByRole('navigation', { name: 'Spending month' })
    fireEvent.click(within(nav).getByRole('button', { name: 'Previous month' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(`Couldn’t load spending for ${monthLabel(prev)}`)
    expect(screen.getByText(/asset allocation/i)).toBeInTheDocument()

    fail = false
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Dining')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('has no month stepper in household scope', async () => {
    setHouseholdScope(1)
    renderPage(<InsightsPage />)
    await screen.findByText(/spending by category · this period/i)
    expect(screen.queryByRole('navigation', { name: 'Spending month' })).toBeNull()
  })
})
