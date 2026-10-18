import { beforeEach, describe, expect, it } from 'vitest'
import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
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
})
