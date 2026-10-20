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

  it('explains why a pre-undo import has no Undo button', async () => {
    const entry = {
      job_id: 1, source: 'csv', file_name: 'old.csv', imported_at: '2026-07-01T00:00:00Z',
      rows_total: 4, rows_imported: 4, rows_duplicate: 0, rows_error: 0, reconciled: null, can_undo: false,
    }
    server.use(http.get(`${API}/accounts/:id/imports`, () => HttpResponse.json({ total: 1, data: [entry] })))
    render(
      <ImportTransactionsModal accounts={fx.accounts} initialAccountID={fx.account1.id} onClose={vi.fn()} onImported={vi.fn()} />,
    )
    expect(await screen.findByText(/Imported before undo was available/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Undo import' })).not.toBeInTheDocument()
  })

  it('undoes an import after an inline confirmation and notifies the caller', async () => {
    const entry = {
      job_id: 3, source: 'csv', file_name: 'july.csv', imported_at: '2026-08-01T00:00:00Z',
      rows_total: 4, rows_imported: 4, rows_duplicate: 0, rows_error: 0, reconciled: null, can_undo: true,
    }
    const undone = vi.fn()
    const after = { ...entry, can_undo: false, undone_at: '2026-08-02T00:00:00Z', rows_undone: 4 }
    let current = entry
    server.use(
      http.get(`${API}/accounts/:id/imports`, () => HttpResponse.json({ total: 1, data: [current] })),
      http.post(`${API}/accounts/:id/imports/3/undo`, () => {
        current = after
        return HttpResponse.json({ data: after })
      }),
    )
    render(
      <ImportTransactionsModal accounts={fx.accounts} initialAccountID={fx.account1.id} onClose={vi.fn()} onImported={vi.fn()} onUndone={undone} />,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Undo import' }))
    expect(screen.getByText(/Remove the 4 transactions/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Yes, undo import' }))
    expect(await screen.findByText(/4 transactions removed/)).toBeInTheDocument()
    expect(undone).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('button', { name: 'Undo import' })).not.toBeInTheDocument()
  })

  it('keeps the entry and shows the error when undo fails', async () => {
    const entry = {
      job_id: 3, source: 'csv', file_name: 'july.csv', imported_at: '2026-08-01T00:00:00Z',
      rows_total: 4, rows_imported: 4, rows_duplicate: 0, rows_error: 0, reconciled: null, can_undo: true,
    }
    server.use(
      http.get(`${API}/accounts/:id/imports`, () => HttpResponse.json({ total: 1, data: [entry] })),
      http.post(`${API}/accounts/:id/imports/3/undo`, () =>
        HttpResponse.json({ error: 'this import cannot be undone', code: 'IMPORT_NOT_UNDOABLE' }, { status: 409 }),
      ),
    )
    renderModal()
    fireEvent.click(await screen.findByRole('button', { name: 'Undo import' }))
    fireEvent.click(screen.getByRole('button', { name: 'Yes, undo import' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/newer import into this account has to be undone first/)
    expect(alert).not.toHaveTextContent(/cannot be undone/)
    expect(screen.getByRole('button', { name: 'Yes, undo import' })).toBeEnabled()
  })

  it('names the newer import that must be undone first, then unblocks the older one (#522)', async () => {
    const newer = {
      job_id: 6, source: 'pdf', file_name: 'june.pdf', imported_at: '2026-07-02T00:00:00Z',
      rows_total: 1, rows_imported: 1, rows_duplicate: 0, rows_error: 0, reconciled: null, can_undo: true,
    }
    const older = {
      job_id: 5, source: 'pdf', file_name: 'may.pdf', imported_at: '2026-06-02T00:00:00Z',
      rows_total: 2, rows_imported: 2, rows_duplicate: 0, rows_error: 0, reconciled: null,
      can_undo: false, undo_blocked_by: 6,
    }
    let undoneNewer = false
    server.use(
      http.get(`${API}/accounts/:id/imports`, () =>
        HttpResponse.json({
          total: 2,
          data: undoneNewer
            ? [{ ...newer, can_undo: false, undone_at: '2026-08-01T00:00:00Z', rows_undone: 1 }, { ...older, can_undo: true, undo_blocked_by: undefined }]
            : [newer, older],
        }),
      ),
      http.post(`${API}/accounts/:id/imports/6/undo`, () => {
        undoneNewer = true
        return HttpResponse.json({ data: { ...newer, can_undo: false, undone_at: '2026-08-01T00:00:00Z', rows_undone: 1 } })
      }),
    )
    renderModal()
    expect(await screen.findByText(/To undo this one, first undo/)).toHaveTextContent(/“june\.pdf”/)
    // Only the newer import offers Undo; the older one explains instead.
    expect(screen.getAllByRole('button', { name: 'Undo import' })).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Undo import' }))
    fireEvent.click(screen.getByRole('button', { name: 'Yes, undo import' }))
    expect(await screen.findByText(/1 transaction removed/)).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText(/To undo this one, first undo/)).not.toBeInTheDocument())
    expect(screen.getAllByRole('button', { name: 'Undo import' })).toHaveLength(1)
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
    expect(section).toHaveTextContent('Opening balance 1,000.00 USD')
    expect(section).toHaveTextContent('unparseable quantity "lots" — skipped')
  })
})
