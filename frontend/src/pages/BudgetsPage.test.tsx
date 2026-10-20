import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
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
})
