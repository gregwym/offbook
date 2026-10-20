import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { useTransactionsStore } from '../store/transactionsStore'
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

    expect(await screen.findByText(/counted in income or spending until you/i)).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: /review it/i }))

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
    expect(await screen.findByText(/matched as a transfer/i)).toBeInTheDocument()
  })

  // #510: a wrong proposal can be rejected. The dismiss endpoint gets both
  // ids, a failed save shows an error and can be retried, and once the
  // server stops proposing the pair the banner goes away.
  it('dismisses a wrong transfer proposal with error and retry', async () => {
    const leg2 = { ...fx.transaction1, id: 2, amount: '50.000000000000000000' }
    let proposed = true
    let attempts = 0
    let dismissedBody: unknown = null
    server.use(
      http.get(`${API}/transfers/candidates`, () =>
        HttpResponse.json({
          data: proposed ? [{ transaction_a: fx.transaction1, transaction_b: leg2 }] : [],
          total: proposed ? 1 : 0,
        }),
      ),
      http.post(`${API}/transfers/dismiss`, async ({ request }) => {
        attempts += 1
        if (attempts === 1) {
          return HttpResponse.json({ error: 'database unavailable', code: 'INTERNAL' }, { status: 500 })
        }
        dismissedBody = await request.json()
        proposed = false
        return new HttpResponse(null, { status: 204 })
      }),
    )

    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    fireEvent.click(await screen.findByRole('button', { name: /review it/i }))

    fireEvent.click(await screen.findByRole('button', { name: /not a transfer/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent(/couldn't save that decision: database unavailable\. try again/i)
    expect(proposed).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: /not a transfer/i }))
    await waitFor(() => {
      expect(dismissedBody).toEqual({ transaction_id_a: fx.transaction1.id, transaction_id_b: leg2.id })
    })
    expect(await screen.findByText(/both transactions now count in income and spending/i)).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /done/i }))
    await waitFor(() => {
      expect(screen.queryByText(/possible transfer found/i)).not.toBeInTheDocument()
    })
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

  // #513: a pending row is marked, and says its edits carry over on posting.
  it('marks only pending rows and explains that edits carry over', async () => {
    const rows = [
      { ...fx.transaction1, id: 31, description: 'CORNER CAFE', source: 'plaid', pending: true },
      { ...fx.transaction1, id: 32, description: 'POSTED GROCER', source: 'plaid', pending: false },
    ]
    server.use(http.get(`${API}/transactions`, () => HttpResponse.json({ data: rows, total: rows.length })))

    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    await screen.findByText('CORNER CAFE')

    const chips = screen.getAllByText('pending', { exact: true })
    expect(chips).toHaveLength(1)
    expect(screen.getByText('CORNER CAFE').parentElement).toContainElement(chips[0])
    expect(chips[0]).toHaveAttribute('title', 'Your bank hasn’t posted this yet. Your edits will carry over when it posts.')
    expect(screen.getByText('POSTED GROCER').parentElement).not.toHaveTextContent(/pending/i)
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

  // ONB-01: an empty ledger with no filters is a first-run state with a next
  // step; with filters it offers a one-click way back to the full list.
  it('distinguishes an empty ledger from filters hiding everything', async () => {
    server.use(http.get(`${API}/transactions`, () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<TransactionsPage />)
    expect(await screen.findByText('No transactions yet.')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /add or import more|add your first account/i })).toHaveAttribute(
      'href',
      '/accounts/add',
    )

    fireEvent.change(screen.getByPlaceholderText(/description or merchant/i), { target: { value: 'zzz' } })
    expect(await screen.findByText('No transactions match these filters.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /clear filters/i }))
    expect(await screen.findByText('No transactions yet.')).toBeInTheDocument()
  })
})

// #518: on mobile only Search shows up front; Account/Category/From/To sit
// behind a Filters toggle (md:hidden) so the list stays above the fold.
describe('TransactionsPage mobile filter toggle (#518)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('keeps secondary filters collapsed below md until the toggle opens them', async () => {
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    const toggle = screen.getByRole('button', { name: /^filters/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveClass('md:hidden')
    const fromField = screen.getByText('From', { selector: 'span' }).closest('label')!
    expect(fromField).toHaveClass('hidden', 'md:block')
    expect(screen.getByPlaceholderText(/description or merchant/i).closest('label')).toHaveClass('block')

    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(fromField).toHaveClass('block')
    expect(fromField).not.toHaveClass('hidden')

    fireEvent.change(fromField.querySelector('input')!, { target: { value: '2026-01-01' } })
    expect(toggle).toHaveTextContent('· 1')
  })

  it('starts open when a secondary filter is already set', async () => {
    useTransactionsStore.setState({ filter: { account_id: 1 } })
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    expect(screen.getByRole('button', { name: /^filters/i })).toHaveAttribute('aria-expanded', 'true')
  })
})

// #544: tags show as chips; a chip filters the list by ?tag=, and the row's
// Tags action opens the editor and refreshes the list after saving.
describe('TransactionsPage tags (#544)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('filters by a tag chip and edits a row’s tags', async () => {
    const rows = [
      { ...fx.transaction1, id: 41, description: 'HOTEL ROMA', tags: ['vacation'] },
      { ...fx.transaction1, id: 42, description: 'GROCER', tags: [] },
    ]
    const seenTags: (string | null)[] = []
    let patched: unknown = null
    server.use(
      http.get(`${API}/transactions`, ({ request }) => {
        const tag = new URL(request.url).searchParams.get('tag')
        seenTags.push(tag)
        const data = tag ? rows.filter((r) => r.tags.includes(tag)) : rows
        return HttpResponse.json({ data, total: data.length })
      }),
      http.patch(`${API}/transactions/42`, async ({ request }) => {
        patched = await request.json()
        return HttpResponse.json({ data: { ...rows[1], tags: ['Work trip'] } })
      }),
    )

    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    await screen.findByText('GROCER')

    fireEvent.click(screen.getByRole('button', { name: 'Show transactions tagged vacation' }))
    await waitFor(() => expect(screen.queryByText('GROCER')).toBeNull())
    expect(seenTags).toContain('vacation')
    const tagSelect = screen.getByText('Tag', { selector: 'span' }).closest('label')!.querySelector('select')!
    expect(tagSelect).toHaveValue('vacation')
    expect(screen.getByRole('button', { name: /^filters/i })).toHaveTextContent('· 1')

    fireEvent.change(tagSelect, { target: { value: '' } })
    await screen.findByText('GROCER')

    fireEvent.click(screen.getAllByRole('button', { name: 'Tags for GROCER' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'Tags for GROCER' })
    expect(dialog).toHaveTextContent('No tags yet.')
    fireEvent.click(screen.getByRole('button', { name: 'Add tag Work trip' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save tags' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patched).toEqual({ tags: ['Work trip'] })
  })
})
