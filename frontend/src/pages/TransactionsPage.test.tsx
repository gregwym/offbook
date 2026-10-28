import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { useTransactionsStore } from '../store/transactionsStore'
import { TransactionsPage } from './TransactionsPage'

const API = '/api/v1'

// A row the bank named and nobody renamed: the raw string is its name.
const bankRow = { ...fx.transaction1, merchant_name: null, description_clean: null }

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
    expect(screen.queryByText('Merchant 4')).not.toBeInTheDocument()

    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    expect(screen.getByText('3 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^mark reviewed$/i }))

    expect(await screen.findByText(/you.re all caught up/i)).toBeInTheDocument()
    expect(reviewBodies).toEqual([{ ids: [1, 2, 3], reviewed: true }])
    await waitFor(() => expect(screen.getByRole('button', { name: /needs review/i })).not.toHaveTextContent('3'))

    // Accidental click? One-click undo brings all three back.
    fireEvent.click(screen.getByRole('button', { name: /undo/i }))
    expect(await screen.findByText('Merchant 1')).toBeInTheDocument()
    expect(screen.getByText('3 to review')).toBeInTheDocument()
    expect(reviewBodies[1]).toEqual({ ids: [1, 2, 3], reviewed: false })
  })

  // #633: inbox zero is only claimed once the server has saved it, so a
  // reload (or leaving the page) right after the click can't lose the write.
  it('says saving, not all caught up, until the review is saved', async () => {
    const { rows } = installReviewApi()
    let release: () => void = () => {}
    const gate = new Promise<void>((r) => { release = r })
    server.use(
      http.post(`${API}/transactions/review`, async ({ request }) => {
        const body = (await request.json()) as { ids: number[]; reviewed: boolean }
        await gate
        for (const t of rows) {
          if (body.ids.includes(t.id)) t.reviewed_at = body.reviewed ? '2026-07-02T00:00:00Z' : (null as unknown as string)
        }
        return HttpResponse.json({ data: { updated: body.ids.length } })
      }),
    )
    renderPage(<TransactionsPage />, '/transactions?review=1')
    await screen.findByText('3 to review')
    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    fireEvent.click(screen.getByRole('button', { name: /^mark reviewed$/i }))

    expect(await screen.findByText(/saving your review/i)).toBeInTheDocument()
    expect(screen.queryByText(/you.re all caught up/i)).not.toBeInTheDocument()

    release()
    expect(await screen.findByText(/you.re all caught up/i)).toBeInTheDocument()
    expect(screen.queryByText(/saving your review/i)).not.toBeInTheDocument()
    expect(rows.filter((t) => t.reviewed_at == null)).toHaveLength(0)
  })

  it('restores the Needs review inbox from the URL after a reload', async () => {
    installReviewApi()
    renderPage(<TransactionsPage />, '/transactions?review=1')
    await screen.findByText('3 to review')
    expect(screen.getByRole('button', { name: /needs review/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText('Merchant 4')).not.toBeInTheDocument()
  })

  it('reviews a single row without touching its category', async () => {
    const { reviewBodies } = installReviewApi()
    renderPage(<TransactionsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /needs review/i }))
    await screen.findByText('3 to review')

    // Phone and desktop render the same toggle; only one shows at a time.
    fireEvent.click((await screen.findAllByRole('button', { name: /mark merchant 2 reviewed/i }))[0])
    await screen.findByText('2 to review')
    expect(reviewBodies).toEqual([{ ids: [2], reviewed: true }])
  })

  it('reverts and surfaces an error when the review call fails', async () => {
    installReviewApi()
    server.use(http.post(`${API}/transactions/review`, () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderPage(<TransactionsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /needs review/i }))
    await screen.findByText('3 to review')

    // Phone and desktop render the same toggle; only one shows at a time.
    fireEvent.click((await screen.findAllByRole('button', { name: /mark merchant 1 reviewed/i }))[0])
    expect(await screen.findByText('boom')).toBeInTheDocument()
    expect(screen.getByText('3 to review')).toBeInTheDocument()
    expect(screen.getByText('Merchant 1')).toBeInTheDocument()
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
      { ...bankRow, id: 11, description: 'CARD PAYMENT', category_id: 9, is_transfer: false },
      { ...bankRow, id: 12, description: 'PAIRED LEG', category_id: 9, is_transfer: true },
      { ...bankRow, id: 13, description: 'GROCER', category_id: 1, is_transfer: false },
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
      { ...bankRow, id: 31, description: 'CORNER CAFE', source: 'plaid', pending: true },
      { ...bankRow, id: 32, description: 'POSTED GROCER', source: 'plaid', pending: false },
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
    const rows = [{ ...bankRow, id: 21, description: 'ONE OFF', is_transfer: false, hidden_at: null }]
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

    chooseRowAction('ONE OFF', 'Hide ONE OFF')
    await screen.findByText('hidden')
    expect(body).toEqual({ hidden: true })
    fireEvent.click(screen.getAllByRole('button', { name: 'More actions for ONE OFF' })[0])
    expect(within(screen.getByRole('menu')).getByRole('menuitem', { name: 'Unhide ONE OFF' })).toBeInTheDocument()
  })

  // #643: a transfer leg is already out of spending, so it offers no Hide;
  // a leg hidden before it paired can still be unhidden.
  it('offers no Hide on transfer legs, but keeps Unhide on a hidden one', async () => {
    const transferCat = { ...fx.categoryGroceries, id: 9, name: 'Transfer', slug: 'transfer' }
    const rows = [
      { ...bankRow, id: 41, description: 'PAIRED LEG', is_transfer: true, hidden_at: null },
      { ...bankRow, id: 42, description: 'CARD PAYMENT', category_id: 9, is_transfer: false, hidden_at: null },
      { ...bankRow, id: 43, description: 'OLD HIDDEN LEG', is_transfer: true, hidden_at: '2026-05-05T00:00:00Z' },
      { ...bankRow, id: 44, description: 'GROCER', category_id: 1, is_transfer: false, hidden_at: null },
    ]
    server.use(
      http.get(`${API}/categories`, () => HttpResponse.json({ data: [...fx.categories, transferCat], total: 3 })),
      http.get(`${API}/transactions`, () => HttpResponse.json({ data: rows, total: rows.length })),
    )

    renderPage(<TransactionsPage />)
    await screen.findByText('PAIRED LEG')
    await screen.findByText('not in spending')
    const menuLabels = (label: string) => {
      fireEvent.click(screen.getAllByRole('button', { name: `More actions for ${label}` })[0])
      const names = within(screen.getByRole('menu')).getAllByRole('menuitem').map((i) => i.getAttribute('aria-label'))
      fireEvent.keyDown(screen.getByRole('menu'), { key: 'Escape' })
      return names
    }
    expect(menuLabels('PAIRED LEG')).not.toContain('Hide PAIRED LEG')
    expect(menuLabels('CARD PAYMENT')).not.toContain('Hide CARD PAYMENT')
    expect(menuLabels('OLD HIDDEN LEG')).toContain('Unhide OLD HIDDEN LEG')
    expect(menuLabels('GROCER')).toContain('Hide GROCER')
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

  // #554: an active filter no longer re-opens the stack; a removable chip
  // says what is applied instead.
  it('stays collapsed with a filter set and shows a removable chip', async () => {
    useTransactionsStore.setState({ filter: { tag: 'reimbursable' } })
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    const toggle = screen.getByRole('button', { name: /^filters/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveTextContent('· 1')
    const chips = screen.getByRole('list', { name: 'Active filters' })
    expect(chips).toHaveClass('md:hidden')
    fireEvent.click(within(chips).getByRole('button', { name: 'Remove filter Tag: reimbursable' }))
    expect(screen.queryByRole('list', { name: 'Active filters' })).not.toBeInTheDocument()
    expect(toggle).not.toHaveTextContent('· 1')
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
      { ...bankRow, id: 41, description: 'HOTEL ROMA', tags: ['vacation'] },
      { ...bankRow, id: 42, description: 'GROCER', tags: [] },
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

    chooseRowAction('GROCER', 'Tags for GROCER')
    const dialog = await screen.findByRole('dialog', { name: 'Tags for GROCER' })
    expect(dialog).toHaveTextContent('No tags yet.')
    fireEvent.click(screen.getByRole('button', { name: 'Add tag Work trip' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save tags' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(patched).toEqual({ tags: ['Work trip'] })
  })
})

// #569: a row with a note shows a "note" chip (tooltip on desktop, written
// out on a phone) that opens Edit details.
describe('TransactionsPage notes (#569)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('shows the note and opens Edit details from the chip', async () => {
    const row = { ...fx.transaction1, id: 52, description: 'NOTE CAFE', notes: 'Coffee with Sam', tags: [] }
    server.use(http.get(`${API}/transactions`, () => HttpResponse.json({ data: [row], total: 1 })))
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    const chip = await screen.findByRole('button', { name: 'Note: Coffee with Sam. Edit details for NOTE CAFE' })
    expect(chip).toHaveAttribute('title', 'Coffee with Sam')
    // Phone layout: the note itself is the tap target.
    expect(screen.getByRole('button', { name: 'Coffee with Sam' })).toBeInTheDocument()
    fireEvent.click(chip)
    expect(await screen.findByRole('dialog', { name: 'Edit details' })).toBeInTheDocument()
    expect(screen.getByLabelText('Notes')).toHaveValue('Coffee with Sam')
  })
})

// #554: on a phone the row keeps category + reviewed inline and folds the
// rest into one "More actions" menu with keyboard support.
describe('TransactionsPage row actions menu (#554)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('opens a menu of secondary actions, moves focus in and back out', async () => {
    const row = { ...fx.transaction1, id: 51, description: 'CORNER CAFE', tags: [] }
    server.use(http.get(`${API}/transactions`, () => HttpResponse.json({ data: [row], total: 1 })))
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    await screen.findAllByText('CORNER CAFE')

    // One trigger per layout (phone block + desktop cell); jsdom renders both.
    const triggers = screen.getAllByRole('button', { name: 'More actions for CORNER CAFE' })
    expect(triggers).toHaveLength(2)
    const more = triggers[0]
    expect(more).toHaveAttribute('aria-haspopup', 'menu')
    expect(more).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(more)
    const menu = screen.getByRole('menu', { name: 'Actions for CORNER CAFE' })
    // Portalled out of the table so a dimmed (hidden) row can't cover it.
    expect(menu.closest('table')).toBeNull()
    const items = within(menu).getAllByRole('menuitem')
    expect(items.map((i) => i.getAttribute('aria-label'))).toEqual([
      'Edit details for CORNER CAFE',
      'Create rule from CORNER CAFE',
      'Split CORNER CAFE',
      'Tags for CORNER CAFE',
      'Hide CORNER CAFE',
      'Delete CORNER CAFE',
    ])
    expect(items[0]).toHaveFocus()
    fireEvent.keyDown(menu, { key: 'ArrowUp' })
    expect(items[5]).toHaveFocus()
    fireEvent.keyDown(menu, { key: 'Escape' })
    expect(screen.queryByRole('menu')).toBeNull()
    expect(more).toHaveFocus()

    // Edit details (#569) opens the merchant/notes dialog.
    fireEvent.click(more)
    fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name: 'Edit details for CORNER CAFE' }))
    expect(await screen.findByRole('dialog', { name: 'Edit details' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog', { name: 'Edit details' })).toBeNull()

    // Choosing an item closes the menu and runs the action.
    fireEvent.click(more)
    fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name: 'Tags for CORNER CAFE' }))
    expect(screen.queryByRole('menu')).toBeNull()
    expect(await screen.findByRole('dialog', { name: 'Tags for CORNER CAFE' })).toBeInTheDocument()
  })
})

// #563: desktop keeps only Reviewed inline; every other row action sits in
// the same menu as on a phone, so the actions cell is one line of controls.
describe('TransactionsPage desktop row actions (#563)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('shows Reviewed and More actions inline, nothing else', async () => {
    const row = { ...fx.transaction1, id: 52, description: 'DESK CAFE', tags: [], reviewed_at: null }
    server.use(http.get(`${API}/transactions`, () => HttpResponse.json({ data: [row], total: 1 })))
    renderPage(<TransactionsPage />)
    await screen.findAllByText('DESK CAFE')

    const desktopCell = screen.getAllByRole('button', { name: 'More actions for DESK CAFE' })[1].closest('td')!
    expect(within(desktopCell).getAllByRole('button').map((b) => b.getAttribute('aria-label'))).toEqual([
      'Mark DESK CAFE reviewed',
      'More actions for DESK CAFE',
    ])
    fireEvent.click(within(desktopCell).getByRole('button', { name: 'More actions for DESK CAFE' }))
    expect(within(screen.getByRole('menu')).getAllByRole('menuitem').map((i) => i.getAttribute('aria-label'))).toEqual([
      'Edit details for DESK CAFE',
      'Create rule from DESK CAFE',
      'Split DESK CAFE',
      'Tags for DESK CAFE',
      'Hide DESK CAFE',
      'Delete DESK CAFE',
    ])
  })
})

function chooseRowAction(label: string, name: string) {
  fireEvent.click(screen.getAllByRole('button', { name: `More actions for ${label}` })[0])
  fireEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', { name }))
}

describe('TransactionsPage bulk edit (#567)', () => {
  beforeEach(() => {
    resetStores()
  })

  function installBulkApi() {
    const rows = [1, 2, 3].map((id) => ({
      ...fx.transaction1,
      id,
      description: `SHOP ${id}`,
      merchant_name: `Shop ${id}`,
      category_id: null as number | null,
      hidden_at: null as string | null,
    }))
    const bodies: unknown[] = []
    server.use(
      http.get(`${API}/transactions`, () => HttpResponse.json({ data: rows, total: rows.length })),
      http.post(`${API}/transactions/bulk-edit`, async ({ request }) => {
        const body = (await request.json()) as { ids: number[]; category_id?: number; hidden?: boolean }
        bodies.push(body)
        const undo = rows.filter((t) => body.ids.includes(t.id)).map((t) => ({
          id: t.id, category_id: t.category_id, categorization_method: null, categorization_rule_id: null,
          hidden_at: t.hidden_at, updated_at: '2026-10-21T00:00:00Z',
        }))
        for (const t of rows) {
          if (!body.ids.includes(t.id)) continue
          if (body.category_id !== undefined) t.category_id = body.category_id
          if (body.hidden !== undefined) t.hidden_at = body.hidden ? '2026-10-21T00:00:00Z' : null
        }
        return HttpResponse.json({ data: { updated: undo.length, skipped_split: 0, undo } })
      }),
      http.post(`${API}/transactions/bulk-edit/undo`, async ({ request }) => {
        const body = (await request.json()) as { undo: { id: number; category_id: number | null; hidden_at: string | null }[] }
        bodies.push(body)
        for (const p of body.undo) {
          const t = rows.find((r) => r.id === p.id)!
          t.category_id = p.category_id
          t.hidden_at = p.hidden_at
        }
        return HttpResponse.json({ data: { restored: body.undo.length, kept: 0 } })
      }),
    )
    return { rows, bodies }
  }

  it('sets one category on the selected rows and undoes it', async () => {
    const { rows, bodies } = installBulkApi()
    renderPage(<TransactionsPage />)
    await screen.findAllByText('Shop 1')

    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    const bar = screen.getByRole('region', { name: 'Bulk actions' })
    fireEvent.change(within(bar).getByLabelText('Set category for selected'), { target: { value: String(fx.categoryGroceries.id) } })

    expect(await screen.findByText('3 transactions moved to Groceries.')).toBeInTheDocument()
    expect(bodies[0]).toEqual({ ids: [1, 2, 3], category_id: fx.categoryGroceries.id })
    expect(rows.every((t) => t.category_id === fx.categoryGroceries.id)).toBe(true)
    expect(screen.queryByRole('region', { name: 'Bulk actions' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(await screen.findByText('Undone: 3 transactions restored.')).toBeInTheDocument()
    expect(rows.every((t) => t.category_id === null)).toBe(true)
    await expectHealthySmoke()
  })

  it('hides the selection, then offers Unhide for hidden rows', async () => {
    const { bodies } = installBulkApi()
    renderPage(<TransactionsPage />)
    await screen.findAllByText('Shop 1')

    fireEvent.click(screen.getAllByRole('checkbox', { name: /select shop 2/i })[0])
    let bar = screen.getByRole('region', { name: 'Bulk actions' })
    expect(within(bar).queryByRole('button', { name: /unhide/i })).not.toBeInTheDocument()
    fireEvent.click(within(bar).getByRole('button', { name: /^hide$/i }))
    expect(await screen.findByText('1 transaction hidden from spending and budgets.')).toBeInTheDocument()
    expect(bodies[0]).toEqual({ ids: [2], hidden: true })

    fireEvent.click(screen.getAllByRole('checkbox', { name: /select shop 2/i })[0])
    bar = screen.getByRole('region', { name: 'Bulk actions' })
    expect(within(bar).queryByRole('button', { name: /^hide$/i })).not.toBeInTheDocument()
    fireEvent.click(within(bar).getByRole('button', { name: /unhide/i }))
    expect(await screen.findByText('1 transaction back in spending and budgets.')).toBeInTheDocument()
  })

  // #643: bulk Hide leaves transfer legs out of the request and the count,
  // and doesn't appear when only transfers are selected.
  it('bulk Hide skips transfer legs', async () => {
    const { rows, bodies } = installBulkApi()
    Object.assign(rows[0], { is_transfer: true })
    renderPage(<TransactionsPage />)
    await screen.findAllByText('Shop 1')

    fireEvent.click(screen.getAllByRole('checkbox', { name: /select shop 1/i })[0])
    let bar = screen.getByRole('region', { name: 'Bulk actions' })
    expect(within(bar).queryByRole('button', { name: /^hide$/i })).not.toBeInTheDocument()

    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    bar = screen.getByRole('region', { name: 'Bulk actions' })
    fireEvent.click(within(bar).getByRole('button', { name: /^hide$/i }))
    expect(await screen.findByText('2 transactions hidden from spending and budgets.')).toBeInTheDocument()
    expect(bodies[0]).toEqual({ ids: [2, 3], hidden: true })
    expect(rows[0].hidden_at).toBeNull()
  })

  it('shows the failure banner and keeps the selection when the edit fails', async () => {
    installBulkApi()
    server.use(http.post(`${API}/transactions/bulk-edit`, () => HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 })))
    renderPage(<TransactionsPage />)
    await screen.findAllByText('Shop 1')
    fireEvent.click(screen.getByLabelText(/select all transactions on this page/i))
    fireEvent.click(within(screen.getByRole('region', { name: 'Bulk actions' })).getByRole('button', { name: /^hide$/i }))
    await waitFor(() => expect(document.querySelector('.border-red-200.bg-red-50')).not.toBeNull())
    expect(screen.getByText('3 selected')).toBeInTheDocument()
  })
})

// Spending drill-down (#571): a link from Insights/Budgets lands on the rows
// behind the figure, with their total, and the view survives a reload.
describe('TransactionsPage spending drill-down', () => {
  beforeEach(() => {
    resetStores()
  })

  it('seeds the spending lens from the URL and shows the figure it explains', async () => {
    const seen: URLSearchParams[] = []
    server.use(
      http.get(`${API}/transactions`, ({ request }) => {
        const q = new URL(request.url).searchParams
        // The review-count probe (reviewed=false&limit=1) is a separate query.
        if (q.get('reviewed') !== 'false') seen.push(q)
        return HttpResponse.json({
          data: fx.transactions,
          total: fx.transactions.length,
          ...(q.get('spending') === 'true' ? { spend_total: '123.45' } : {}),
        })
      }),
    )
    renderPage(<TransactionsPage />, '/transactions?spending=1&category_id=1&from=2026-10-01&to=2026-10-31')

    const banner = await screen.findByRole('region', { name: /spending drill-down/i })
    await waitFor(() => expect(within(banner).getByText(/Groceries/)).toBeInTheDocument())
    expect(within(banner).getByTestId('spend-total')).toHaveTextContent('123.45')
    const last = seen[seen.length - 1]
    expect(last.get('spending')).toBe('true')
    expect(last.get('category_id')).toBe('1')
    expect(last.get('from')).toBe('2026-10-01')
    expect(last.get('to')).toBe('2026-10-31')

    fireEvent.click(within(banner).getByRole('button', { name: /show all transactions/i }))
    await waitFor(() => expect(screen.queryByRole('region', { name: /spending drill-down/i })).not.toBeInTheDocument())
    await waitFor(() => {
      const q = seen[seen.length - 1]
      expect(q.get('spending')).toBeNull()
      expect(q.get('category_id')).toBeNull()
      expect(q.get('from')).toBeNull()
    })
  })

  it('passes a group budget drill-down subcategories through and names them (#596)', async () => {
    const produce = { ...fx.categoryGroceries, id: 7, parent_id: 1, name: 'Produce', slug: 'produce', is_system: false }
    const subcats: (string | null)[] = []
    server.use(
      http.get(`${API}/categories`, () => HttpResponse.json({ data: [...fx.categories, produce], total: 3 })),
      http.get(`${API}/transactions`, ({ request }) => {
        const q = new URL(request.url).searchParams
        if (q.get('spending')) subcats.push(q.get('subcategory_ids'))
        return HttpResponse.json({ data: fx.transactions, total: 1, spend_total: '20' })
      }),
    )
    renderPage(<TransactionsPage />, '/transactions?spending=1&category_id=1&subcategory_ids=7&from=2026-10-01&to=2026-10-31')
    const banner = await screen.findByRole('region', { name: /spending drill-down/i })
    await waitFor(() => expect(within(banner).getByText('Groceries (incl. Produce)')).toBeInTheDocument())
    await waitFor(() => expect(subcats.length).toBeGreaterThan(0))
    expect(subcats.every((v) => v === '7')).toBe(true)
  })

  it('passes a merchant drill-down through and labels the banner with it', async () => {
    let merchant: string | null = null
    server.use(
      http.get(`${API}/transactions`, ({ request }) => {
        const q = new URL(request.url).searchParams
        if (q.get('reviewed') !== 'false') merchant = q.get('merchant')
        return HttpResponse.json({ data: fx.transactions, total: 1, spend_total: '9.99' })
      }),
    )
    renderPage(<TransactionsPage />, '/transactions?spending=1&merchant=Blue+Bottle&from=2026-10-01&to=2026-10-31')
    const banner = await screen.findByRole('region', { name: /spending drill-down/i })
    expect(within(banner).getByText('Blue Bottle')).toBeInTheDocument()
    await waitFor(() => expect(merchant).toBe('Blue Bottle'))
  })
})

// #579: rows lead with the name the user recognises; the bank string stays visible.
describe('TransactionsPage row name (#579)', () => {
  beforeEach(() => {
    resetStores()
  })

  function rowFor(text: string) {
    return screen.getAllByText(text)[0].closest('tr')!
  }

  it('leads with the merchant, then the cleaned name, then the bank string', async () => {
    server.use(
      http.get(`${API}/transactions`, () => HttpResponse.json({
        data: [
          { ...fx.transaction1, id: 61, description: 'QAEDIT SQ *BLUE BTL 0415', description_clean: 'Sq Blue Btl', merchant_name: 'Blue Bottle' },
          { ...fx.transaction1, id: 62, description: 'TST* CORNER 22', description_clean: 'Corner Bakery', merchant_name: null },
          { ...fx.transaction1, id: 63, description: 'ACH DEPOSIT 991', description_clean: null, merchant_name: '' },
        ],
        total: 3,
      })),
    )
    renderPage(<TransactionsPage />)
    await expectHealthySmoke()
    await screen.findAllByText('Blue Bottle')

    const first = rowFor('Blue Bottle')
    // Primary label is the first cell after the checkbox/date; it carries the merchant.
    const primary = first.querySelector('td:nth-child(3) span.font-medium')!
    expect(primary).toHaveTextContent('Blue Bottle')
    // The raw bank string is still in the row (desktop column + phone meta line).
    expect(within(first).getAllByText('QAEDIT SQ *BLUE BTL 0415').length).toBeGreaterThan(0)
    // Accessible names keep the bank string so existing journeys still address the row.
    expect(within(first).getAllByRole('combobox', { name: 'Category for QAEDIT SQ *BLUE BTL 0415' }).length).toBeGreaterThan(0)

    const second = rowFor('Corner Bakery')
    expect(second.querySelector('td:nth-child(3) span.font-medium')).toHaveTextContent('Corner Bakery')
    expect(within(second).getAllByText('TST* CORNER 22').length).toBeGreaterThan(0)

    const third = rowFor('ACH DEPOSIT 991')
    expect(third.querySelector('td:nth-child(3) span.font-medium')).toHaveTextContent('ACH DEPOSIT 991')
    // No secondary repeat in the phone meta line when the name is the bank string.
    // The bank string is the name, so it isn't repeated in the column or meta line.
    expect(within(third).getAllByText('ACH DEPOSIT 991')).toHaveLength(1)
  })
})

describe('TransactionsPage account filter (#657)', () => {
  beforeEach(() => {
    resetStores()
  })

  function LocationProbe() {
    const loc = useLocation()
    return <div data-testid="location">{loc.pathname + loc.search}</div>
  }

  it('reads account_id from the URL over the stored filter and shows the account header', async () => {
    const card = { ...fx.account1, id: 3, name: 'QAM Card', account_type: 'credit_card' as const, balance: '-12341.700000000000000000' }
    const seen: (string | null)[] = []
    server.use(
      http.get(`${API}/accounts`, () => HttpResponse.json({ data: [fx.account1, card], total: 2 })),
      http.get(`${API}/transactions`, ({ request }) => {
        const id = new URL(request.url).searchParams.get('account_id')
        seen.push(id)
        const rows = id === '3' ? [{ ...fx.transaction1, account_id: 3 }] : fx.transactions
        return HttpResponse.json({ data: rows, total: rows.length })
      }),
    )
    // The store remembers another account; the URL must win.
    useTransactionsStore.setState({ filter: { account_id: 1 } })
    render(
      <MemoryRouter initialEntries={['/transactions?account_id=3']}>
        <TransactionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )
    const header = await screen.findByRole('region', { name: 'Account QAM Card' })
    // Same value and formatting as the Accounts row.
    expect(within(header).getByLabelText('Account balance')).toHaveTextContent('-12,341.70 USD')
    expect(within(header).getByRole('link', { name: 'Reconciliation' })).toHaveAttribute('href', '/accounts/3/reconciliation')
    // A credit card is a manual cash-style account: its balance can be updated.
    expect(within(header).getByRole('button', { name: 'Update balance' })).toBeTruthy()
    expect(screen.getByLabelText('Account')).toHaveValue('3')
    await waitFor(() => expect(useTransactionsStore.getState().filter.account_id).toBe(3))
    expect(seen).toContain('3')
    await waitFor(() => expect(useTransactionsStore.getState().transactions.every((t) => t.account_id === 3)).toBe(true))

    // Clearing the account drops the parameter and the header.
    fireEvent.click(within(header).getByRole('button', { name: 'All accounts' }))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(/^\/transactions$/))
    expect(screen.queryByRole('region', { name: 'Account QAM Card' })).toBeNull()
    await waitFor(() => expect(useTransactionsStore.getState().filter.account_id).toBeUndefined())
  })

  it('restores the stored account into the URL when none is given', async () => {
    useTransactionsStore.setState({ filter: { account_id: 1 } })
    render(
      <MemoryRouter initialEntries={['/transactions']}>
        <TransactionsPage />
        <LocationProbe />
      </MemoryRouter>,
    )
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/transactions?account_id=1'))
    expect(await screen.findByRole('region', { name: 'Account Checking' })).toBeTruthy()
  })
})

