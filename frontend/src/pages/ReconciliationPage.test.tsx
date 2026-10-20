import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { reconciliationReport, transaction1 } from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, resetStores } from '../test/testUtils'
import { ReconciliationPage } from './ReconciliationPage'

function renderReconciliation() {
  render(
    <MemoryRouter initialEntries={['/accounts/1/reconciliation']}>
      <Routes>
        <Route path="/accounts/:id/reconciliation" element={<ReconciliationPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

const flagged = {
  ...reconciliationReport,
  needs_attention: true,
  checkpoints: [
    ...reconciliationReport.checkpoints,
    {
      observation_id: 2,
      asset_id: 1,
      as_of: new Date().toISOString(),
      source: 'manual',
      observed_quantity: '850.000000000000000000',
      prior_fold: '1000.000000000000000000',
      delta: '-150.000000000000000000',
      transaction_id: 2,
      kind: 'adjustment',
      acknowledged: false,
      acknowledged_at: null,
      acknowledged_note: null,
      needs_attention: true,
    },
  ],
}

function stubWidth(wide: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: wide,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
}

describe('ReconciliationPage', () => {
  beforeEach(() => {
    resetStores()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the checkpoint history without a stuck loading state or error banner', async () => {
    renderReconciliation()
    await expectHealthySmoke()
  })

  it('explains reconciliation in plain language, never raw enum names (#512)', async () => {
    server.use(http.get('/api/v1/accounts/:id/reconciliation', () => HttpResponse.json({ data: flagged })))
    renderReconciliation()
    const table = await screen.findByRole('table')
    expect(document.body.textContent).not.toMatch(/opening_balance|Ledger before|\bplaid\b|\bmanual\b/)
    expect(screen.getByText(/compares it with what your transactions add up/)).toBeInTheDocument()
    expect(within(table).getByRole('columnheader', { name: 'Your transactions' })).toBeInTheDocument()
    expect(within(table).getByText('Your bank')).toBeInTheDocument()
    expect(within(table).getByText('You')).toBeInTheDocument()
    expect(within(table).getByText('Opening balance')).toBeInTheDocument()
  })

  it('stacks each observation as a card on narrow screens and acknowledges inline (#512)', async () => {
    stubWidth(false)
    let acked: unknown = null
    server.use(
      http.get('/api/v1/accounts/:id/reconciliation', () => HttpResponse.json({ data: flagged })),
      http.patch('/api/v1/transactions/:id/acknowledge', async ({ request }) => {
        acked = await request.json()
        return HttpResponse.json({ data: transaction1 })
      }),
    )
    renderReconciliation()
    const list = await screen.findByRole('list', { name: 'Recorded balances' })
    expect(screen.queryByRole('table')).toBeNull()
    const cards = within(list).getAllByRole('listitem')
    expect(cards).toHaveLength(2)
    expect(cards[1]).toHaveTextContent('You reported')
    expect(cards[1]).toHaveTextContent('850.00')
    expect(cards[1]).toHaveTextContent('Your transactions added up to')
    expect(cards[1]).toHaveTextContent('1,000.00')
    expect(cards[1]).toHaveTextContent('The difference was recorded as an adjustment.')
    expect(cards[0]).toHaveTextContent('recorded as this account’s opening balance')

    fireEvent.click(within(cards[1]).getByRole('button', { name: /mark as reviewed/ }))
    fireEvent.change(within(cards[1]).getByLabelText('Note'), { target: { value: 'bank fee' } })
    fireEvent.click(within(cards[1]).getByRole('button', { name: 'Mark reviewed' }))
    await vi.waitFor(() => expect(acked).toEqual({ note: 'bank fee' }))
  })
})
