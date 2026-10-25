import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { expectHealthySmoke, renderPage, resetStores, setHouseholdScope } from '../test/testUtils'
import { BudgetsPage } from './BudgetsPage'

describe('BudgetsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the personal-scope budget list without a stuck loading state or error banner', async () => {
    renderPage(<BudgetsPage />)
    await expectHealthySmoke()
  })

  it('shows the rolled-over balance on a rollover budget', async () => {
    server.use(
      http.get('*/budgets', () =>
        HttpResponse.json({ data: [{ ...fx.budget1, rollover: true }], total: 1 }),
      ),
      http.get('*/budgets/:id/spend', () =>
        HttpResponse.json({
          data: { ...fx.budgetSpend1, carryover: '140', available: '640', remaining: '520' },
        }),
      ),
    )
    renderPage(<BudgetsPage />)
    expect(await screen.findByText(/rolled over/i)).toBeTruthy()
  })

  it('words an overspent budget as "over budget" instead of a negative remainder', async () => {
    server.use(
      http.get('*/budgets/:id/spend', () =>
        HttpResponse.json({ data: { ...fx.budgetSpend1, remaining: '-10.00', pct: 1.2 } }),
      ),
    )
    renderPage(<BudgetsPage />)
    expect(await screen.findByText(/over budget/i)).toBeTruthy()
    expect(screen.queryByText(/-10\.00/)).toBeNull()
  })

  it('shows the rolled-over balance on a shared (household) budget', async () => {
    setHouseholdScope(10)
    server.use(
      http.get('*/households/:id/shared-budgets', () =>
        HttpResponse.json({ data: [{ ...fx.budget1, household_id: 10, rollover: true }], total: 1 }),
      ),
      http.get('*/h/budgets/pace', () =>
        HttpResponse.json({
          data: [
            {
              budget_id: fx.budget1.id,
              category_id: fx.budget1.category_id,
              period: 'monthly',
              budget: '500',
              spent: '100',
              carryover: '140',
              available: '640',
              remaining: '540',
              pace: '0.15625',
            },
          ],
          total: 1,
        }),
      ),
    )
    renderPage(<BudgetsPage />)
    expect(await screen.findByText(/rolled over/i)).toBeTruthy()
  })

  it('sends a rollover starting balance and rejects a negative one', async () => {
    let body: Record<string, unknown> | null = null
    server.use(
      http.get('*/budgets', () => HttpResponse.json({ data: [], total: 0 })),
      http.post('*/budgets', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ data: { ...fx.budget1, rollover: true, rollover_start: '40' } }, { status: 201 })
      }),
    )
    renderPage(<BudgetsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new budget/i }))
    expect(screen.queryByLabelText('Starting balance')).toBeNull()
    fireEvent.change(screen.getByLabelText('Category'), { target: { value: String(fx.budget1.category_id) } })
    fireEvent.change(screen.getByLabelText('Amount'), { target: { value: '200' } })
    fireEvent.click(screen.getByRole('checkbox', { name: /roll unspent balance/i }))
    const start = screen.getByLabelText('Starting balance')
    fireEvent.change(start, { target: { value: '-5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText(/starting balance of 0 or more/i)).toBeTruthy()
    expect(body).toBeNull()
    fireEvent.change(start, { target: { value: '40.00' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(body).not.toBeNull())
    expect(body).toMatchObject({ rollover: true, rollover_start: '40.00', amount: '200' })
  })

  it('words the rollover hint so it reads before the starting-balance field appears (#590)', async () => {
    server.use(http.get('*/budgets', () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<BudgetsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /new budget/i }))
    expect(screen.queryByLabelText('Starting balance')).toBeNull()
    expect(screen.getByText('You can set a starting balance. An overspent period resets the balance to zero.')).toBeTruthy()
    expect(screen.queryByText(/balance below/i)).toBeNull()
  })

  it('explains a carryover that includes a starting balance', async () => {
    server.use(
      http.get('*/budgets', () =>
        HttpResponse.json({ data: [{ ...fx.budget1, rollover: true, rollover_start: '40.000000000000000000' }], total: 1 }),
      ),
      http.get('*/budgets/:id/spend', () =>
        HttpResponse.json({ data: { ...fx.budgetSpend1, carryover: '40', available: '540', remaining: '440' } }),
      ),
    )
    renderPage(<BudgetsPage />)
    expect(await screen.findByText(/starting balance plus unspent/i)).toBeTruthy()
  })

  it('steps back to an earlier month, fetching spend for that month, and stops at the current month (#577)', async () => {
    const ons: (string | null)[] = []
    server.use(
      http.get('*/budgets', () =>
        HttpResponse.json({ data: [fx.budget1, { ...fx.budget1, id: 2, category_id: 2, period: 'weekly' }], total: 2 }),
      ),
      http.get('*/budgets/:id/spend', ({ request }) => {
        const on = new URL(request.url).searchParams.get('on')
        ons.push(on)
        return HttpResponse.json({
          data: on
            ? { ...fx.budgetSpend1, period_start: `${on.slice(0, 7)}-01`, spent: '77.000000000000000000' }
            : fx.budgetSpend1,
        })
      }),
    )
    renderPage(<BudgetsPage />)
    const next = await screen.findByRole('button', { name: 'Next month' })
    expect((next as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'This month' })).toBeNull()

    const now = new Date()
    const prev = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1))
    const prevMonth = prev.toISOString().slice(0, 7)
    const lastDay = new Date(Date.UTC(prev.getUTCFullYear(), prev.getUTCMonth() + 1, 0)).toISOString().slice(0, 10)
    const label = prev.toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })

    fireEvent.click(screen.getByRole('button', { name: 'Previous month' }))
    expect(await screen.findByText(label)).toBeTruthy()
    await waitFor(() => expect(ons).toContain(lastDay))
    expect(lastDay.startsWith(prevMonth)).toBe(true)
    expect(await screen.findByText(/77\.00 USD/)).toBeTruthy()
    // The weekly budget is left out of a past month, and the note says so.
    expect(screen.getByText(/1 weekly, annual or paused budget is only shown for the current period/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Next month' }) as HTMLButtonElement).disabled).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'This month' }))
    await waitFor(() => expect((screen.getByRole('button', { name: 'Next month' }) as HTMLButtonElement).disabled).toBe(true))
    await expectHealthySmoke()
  })

  it('shows the month totals and where spending outside budgets went (#589)', async () => {
    renderPage(<BudgetsPage />)
    const strip = await screen.findByRole('region', { name: 'July 2026 totals' })
    const stat = (id: string) => within(strip).getByTestId(id).textContent
    expect(stat('total-budgeted')).toContain('500.00 USD')
    expect(stat('total-spent')).toContain('120.00 USD')
    expect(stat('total-remaining')).toMatch(/Remaining.*380\.00 USD/)
    expect(stat('total-unbudgeted')).toContain('83.50 USD')
    expect(within(strip).getByText(/All spending in July 2026/).textContent).toContain('203.50 USD')

    fireEvent.click(within(strip).getByText('Where the spending outside budgets went'))
    const items = within(strip).getAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([expect.stringContaining('Income'), expect.stringContaining('Uncategorized')])
    expect(within(strip).getByRole('link', { name: 'See Uncategorized transactions' }).getAttribute('href'))
      .toBe('/transactions?spending=1&category_id=null&from=2026-07-01&to=2026-07-31')
    await expectHealthySmoke()
  })

  it('words an overspent month as "Over budget" and asks for the month it shows', async () => {
    const months: (string | null)[] = []
    server.use(
      http.get('*/budgets/summary', ({ request }) => {
        months.push(new URL(request.url).searchParams.get('month'))
        return HttpResponse.json({ data: { ...fx.budgetSummary1, spent_in_budgets: '620', remaining: '-120' } })
      }),
    )
    renderPage(<BudgetsPage />)
    const strip = await screen.findByRole('region', { name: 'July 2026 totals' })
    expect(within(strip).getByTestId('total-remaining').textContent).toMatch(/Over budget.*120\.00 USD/)
    expect(months).toEqual([null])
    fireEvent.click(screen.getByRole('button', { name: 'Previous month' }))
    const now = new Date()
    const prev = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1)).toISOString().slice(0, 7)
    await waitFor(() => expect(months).toContain(prev))
  })

  it('keeps weekly and annual budgets apart from the month under "Other periods"', async () => {
    server.use(
      http.get('*/budgets', () =>
        HttpResponse.json({ data: [fx.budget1, { ...fx.budget1, id: 2, category_id: 2, period: 'weekly' }], total: 2 }),
      ),
    )
    renderPage(<BudgetsPage />)
    const other = await screen.findByRole('region', { name: 'Other periods' })
    expect(within(other).getByText('Income')).toBeTruthy()
    expect(within(other).queryByText('Groceries')).toBeNull()
    expect(within(other).getByText(/aren’t part of this month’s totals/)).toBeTruthy()
  })

  it('a failed summary hides only the strip and offers a retry', async () => {
    server.use(http.get('*/budgets/summary', () => HttpResponse.json({ error: 'boom', code: 'X' }, { status: 500 })))
    renderPage(<BudgetsPage />)
    expect(await screen.findByText(/totals didn’t load/)).toBeTruthy()
    expect(screen.getByText('Groceries')).toBeTruthy()
    expect(screen.queryByRole('region', { name: /totals$/ })).toBeNull()
    server.use(http.get('*/budgets/summary', () => HttpResponse.json({ data: fx.budgetSummary1 })))
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('region', { name: 'July 2026 totals' })).toBeTruthy()
  })
})
