import { beforeEach, describe, expect, it } from 'vitest'
import { screen } from '@testing-library/react'
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
})
