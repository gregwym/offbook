import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { TransactionsPage } from './TransactionsPage'

const API = '/api/v1'

describe('TransactionsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the transaction list without a stuck loading state or error banner', async () => {
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
  })

  // #371: a proposed transfer-match pair surfaces as a banner; opening the
  // review modal and confirming it calls the confirm endpoint with both
  // transaction ids, and nothing links until that click happens.
  it('surfaces a proposed transfer match and confirms it on demand', async () => {
    const leg2 = { ...fx.transaction1, id: 2, amount: '50.000000000000000000' }
    server.use(
      http.get(`${API}/transfers/candidates`, () =>
        HttpResponse.json({
          data: [{ transaction_a: fx.transaction1, transaction_b: leg2 }],
          total: 1,
        }),
      ),
    )

    renderPage(<TransactionsPage />)
    await expectHealthySmoke()

    fireEvent.click(await screen.findByRole('button', { name: /review matches/i }))

    expect(await screen.findByRole('button', { name: /confirm match/i })).toBeInTheDocument()

    let confirmedBody: unknown = null
    server.use(
      http.post(`${API}/transfers/confirm`, async ({ request }) => {
        confirmedBody = await request.json()
        return HttpResponse.json({
          data: { transaction_a: { ...fx.transaction1, is_transfer: true }, transaction_b: { ...leg2, is_transfer: true } },
        })
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: /confirm match/i }))

    await waitFor(() => {
      expect(confirmedBody).toEqual({ transaction_id_a: fx.transaction1.id, transaction_id_b: leg2.id })
    })
    expect(await screen.findByRole('button', { name: /matched/i })).toBeInTheDocument()
  })
})
