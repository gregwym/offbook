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

// #424: the daily review journey against a stateful fake of the review API.
describe('TransactionsPage review inbox (#424)', () => {
  beforeEach(() => {
    resetStores()
  })

  function installReviewApi() {
    const rows = [1, 2, 3, 4].map((id) => ({
      ...fx.transaction1,
      id,
      description: `MERCHANT ${id}`,
      merchant_name: `Merchant ${id}`,
      // Row 4 starts reviewed; 1-3 are the inbox. Row 1 is already
      // categorized 'manual' — categorization must not imply reviewed.
      reviewed_at: id === 4 ? '2026-07-01T00:00:00Z' : null,
    }))
    const reviewBodies: unknown[] = []
    server.use(
      http.get(`${API}/transactions`, ({ request }) => {
        const r = new URL(request.url).searchParams.get('reviewed')
        const data = rows.filter((t) => r == null || (r === 'false') === (t.reviewed_at == null))
        return HttpResponse.json({ data, total: data.length })
      }),
      http.post(`${API}/transactions/review`, async ({ request }) => {
        const body = (await request.json()) as { ids: number[]; reviewed: boolean }
        reviewBodies.push(body)
        for (const t of rows) {
          if (body.ids.includes(t.id)) t.reviewed_at = body.reviewed ? '2026-07-02T00:00:00Z' : (null as unknown as string)
        }
        return HttpResponse.json({ data: { updated: body.ids.length } })
      }),
    )
    return { rows, reviewBodies }
  }

  it('shows the inbox count, bulk-reviews to inbox zero, and undoes', async () => {
    const { reviewBodies } = installReviewApi()
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()

    const chip = await screen.findByRole('button', { name: /needs review/i })
    await waitFor(() => expect(chip).toHaveTextContent('3'))

    fireEvent.click(chip)
    await screen.findByText('3 to review')
    expect(screen.queryByText('MERCHANT 4')).not.toBeInTheDocument()

    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    expect(screen.getByText('3 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^mark reviewed$/i }))

    expect(await screen.findByText(/you.re all caught up/i)).toBeInTheDocument()
    expect(reviewBodies).toEqual([{ ids: [1, 2, 3], reviewed: true }])
    await waitFor(() => expect(screen.getByRole('button', { name: /needs review/i })).not.toHaveTextContent('3'))

    // Accidental click? One-click undo brings all three back.
    fireEvent.click(screen.getByRole('button', { name: /undo/i }))
    expect(await screen.findByText('MERCHANT 1')).toBeInTheDocument()
    expect(screen.getByText('3 to review')).toBeInTheDocument()
    expect(reviewBodies[1]).toEqual({ ids: [1, 2, 3], reviewed: false })
  })

  it('restores the Needs review inbox from the URL after a reload', async () => {
    installReviewApi()
    renderPage(<TransactionsPage />, '/transactions?review=1')
    await screen.findByText('3 to review')
    expect(screen.getByRole('button', { name: /needs review/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText('MERCHANT 4')).not.toBeInTheDocument()
  })

  it('reviews a single row without touching its category', async () => {
    const { reviewBodies } = installReviewApi()
    renderPage(<TransactionsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /needs review/i }))
    await screen.findByText('3 to review')

    fireEvent.click(await screen.findByRole('button', { name: /mark merchant 2 reviewed/i }))
    await screen.findByText('2 to review')
    expect(reviewBodies).toEqual([{ ids: [2], reviewed: true }])
  })

  it('reverts and surfaces an error when the review call fails', async () => {
    installReviewApi()
    server.use(http.post(`${API}/transactions/review`, () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderPage(<TransactionsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /needs review/i }))
    await screen.findByText('3 to review')

    fireEvent.click(await screen.findByRole('button', { name: /mark merchant 1 reviewed/i }))
    expect(await screen.findByText('boom')).toBeInTheDocument()
    expect(screen.getByText('3 to review')).toBeInTheDocument()
    expect(screen.getByText('MERCHANT 1')).toBeInTheDocument()
  })
})

// #463: an unpaired Transfer-category row is excluded from spending (#461) and
// the list must say so; confirmed pairs and other categories get no such chip.
describe('TransactionsPage unpaired transfer chip (#463)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('flags only unpaired Transfer-category flow rows as not in spending', async () => {
    const transferCat = { ...fx.categoryGroceries, id: 9, name: 'Transfer', slug: 'transfer' }
    const rows = [
      { ...fx.transaction1, id: 11, description: 'CARD PAYMENT', category_id: 9, is_transfer: false },
      { ...fx.transaction1, id: 12, description: 'PAIRED LEG', category_id: 9, is_transfer: true },
      { ...fx.transaction1, id: 13, description: 'GROCER', category_id: 1, is_transfer: false },
    ]
    server.use(
      http.get(`${API}/categories`, () => HttpResponse.json({ data: [...fx.categories, transferCat], total: 3 })),
      http.get(`${API}/transactions`, () => HttpResponse.json({ data: rows, total: rows.length })),
    )

    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    await screen.findByText('CARD PAYMENT')

    expect(screen.getAllByText('not in spending')).toHaveLength(1)
    expect(screen.getByText('CARD PAYMENT').parentElement).toHaveTextContent('not in spending')
  })

  it('hides and unhides a flow row via PATCH and shows a hidden chip', async () => {
    const rows = [{ ...fx.transaction1, id: 21, description: 'ONE OFF', is_transfer: false, hidden_at: null }]
    let body: unknown = null
    server.use(
      http.get(`${API}/transactions`, () => HttpResponse.json({ data: rows, total: 1 })),
      http.patch(`${API}/transactions/21`, async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ data: { ...rows[0], hidden_at: '2026-05-05T00:00:00Z' } })
      }),
    )

    renderPage(<TransactionsPage />)
    await screen.findByText('ONE OFF')
    expect(screen.queryByText('hidden')).toBeNull()

    fireEvent.click(screen.getAllByRole('button', { name: 'Hide ONE OFF' })[0])
    await screen.findByText('hidden')
    expect(body).toEqual({ hidden: true })
    expect(screen.getAllByRole('button', { name: 'Unhide ONE OFF' }).length).toBeGreaterThan(0)
  })
})
