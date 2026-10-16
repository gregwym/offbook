import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { ImportTransactionsModal } from './ImportTransactionsModal'

const API = '/api/v1'

function renderModal() {
  return render(
    <ImportTransactionsModal accounts={fx.accounts} initialAccountID={fx.account1.id} onClose={vi.fn()} onImported={vi.fn()} />,
  )
}

describe('ImportTransactionsModal import history (#376)', () => {
  it('shows empty state when the account has no imports', async () => {
    renderModal()
    expect(await screen.findByText('No imports into this account yet.')).toBeInTheDocument()
  })

  it('lists past imports and calls out a statement-total mismatch', async () => {
    server.use(
      http.get(`${API}/accounts/:id/imports`, () =>
        HttpResponse.json({
          total: 2,
          data: [
            {
              job_id: 2, source: 'pdf', file_name: 'june.pdf', imported_at: '2026-07-01T00:00:00Z',
              rows_total: 10, rows_imported: 9, rows_duplicate: 1, rows_error: 0,
              reconciled: false, difference: '4.5', doc_totals: ['100'], row_sum: '95.5',
            },
            {
              job_id: 1, source: 'csv', file_name: 'may.csv', imported_at: '2026-06-01T00:00:00Z',
              rows_total: 5, rows_imported: 5, rows_duplicate: 0, rows_error: 0, reconciled: null,
            },
          ],
        }),
      ),
    )
    renderModal()
    expect(await screen.findByText('june.pdf')).toBeInTheDocument()
    expect(screen.getByText('may.csv')).toBeInTheDocument()
    expect(screen.getByText(/Didn’t match statement total/)).toBeInTheDocument()
    expect(screen.getByText(/9 imported · 1 duplicate/)).toBeInTheDocument()
  })

  it('degrades to a quiet message when history fails to load', async () => {
    server.use(http.get(`${API}/accounts/:id/imports`, () => HttpResponse.json({ error: 'x', code: 'X' }, { status: 500 })))
    renderModal()
    expect(await screen.findByText(/Couldn’t load import history/)).toBeInTheDocument()
  })

  it('blocks a duplicate AI extraction with an explicit override', async () => {
    let calls = 0
    server.use(
      http.post(`${API}/accounts/:id/transactions/import/extract`, async ({ request }) => {
        calls++
        const form = await request.formData()
        if (form.get('allow_duplicate') !== 'true') {
          return HttpResponse.json(
            { error: 'dup', code: 'DUPLICATE_STATEMENT', prior: { job_id: 1 } },
            { status: 409 },
          )
        }
        return HttpResponse.json({
          data: {
            committed: false, source: 'pdf', mapping: { date: '', amount: '', description: '' }, headers: [],
            total_rows: 0, new_count: 0, duplicate_count: 0, error_count: 0, review_count: 0, inserted_count: 0,
            rows: [], job_id: 9,
          },
        })
      }),
    )
    const { container } = renderModal()
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['%PDF'], 'stmt.pdf', { type: 'application/pdf' })] } })
    fireEvent.click(screen.getByRole('button', { name: /Extract with AI/ }))
    expect(await screen.findByText(/already imported this exact statement/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Extract anyway' }))
    await waitFor(() => expect(calls).toBe(2))
    await waitFor(() => expect(screen.queryByText(/already imported this exact statement/)).not.toBeInTheDocument())
  })

  it('previews statement holdings and the ledger effect before commit (#375)', async () => {
    server.use(
      http.post(`${API}/accounts/:id/transactions/import/extract`, () =>
        HttpResponse.json({
          data: {
            committed: false, source: 'pdf', mapping: { date: '', amount: '', description: '' }, headers: [],
            total_rows: 0, new_count: 0, duplicate_count: 0, error_count: 0, review_count: 1, inserted_count: 0,
            rows: [], job_id: 11,
            closing_balance: {
              symbol: 'USD', reported: '1100.00', as_of: '2026-05-31T00:00:00Z',
              effect: { current_quantity: '100', adjustment: '1000', kind: 'opening_balance' },
            },
            positions: [
              { line: 1, symbol: 'VTI', asset_kind: 'fund', quantity: '12.5', as_of: '2026-05-31T00:00:00Z',
                confidence: 0.95, needs_review: false, asset_status: 'new',
                effect: { current_quantity: '0', adjustment: '12.5', kind: 'opening_balance' } },
              { line: 2, symbol: 'BAD', asset_kind: 'fund', quantity: '0', as_of: '2026-05-31T00:00:00Z',
                confidence: 0.9, needs_review: false, error: 'unparseable quantity "lots"' },
            ],
          },
        }),
      ),
    )
    const { container } = renderModal()
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['%PDF'], 'stmt.pdf', { type: 'application/pdf' })] } })
    fireEvent.click(screen.getByRole('button', { name: /Extract with AI/ }))
    const section = await screen.findByRole('region', { name: 'Statement holdings' })
    expect(section).toHaveTextContent('VTI')
    expect(section).toHaveTextContent('new asset')
    expect(section).toHaveTextContent('Opening balance +12.5')
    expect(section).toHaveTextContent('Opening balance +1000')
    expect(section).toHaveTextContent('unparseable quantity "lots" — skipped')
  })
})
