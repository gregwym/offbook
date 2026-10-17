import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { SplitTransactionModal } from './SplitTransactionModal'

const tx = { ...fx.transaction1, amount: '-100.10' }

function setup() {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  render(<SplitTransactionModal tx={tx} currency="USD" categories={fx.categories} onClose={onClose} onSaved={onSaved} />)
  return { onSaved, onClose }
}

describe('SplitTransactionModal (#438)', () => {
  it('blocks save until the parts sum exactly, then sends parent-signed amounts', async () => {
    let body: unknown
    server.use(
      http.put('/api/v1/transactions/:id/splits', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ data: { ...tx, splits: [] } })
      }),
    )
    const { onSaved } = setup()
    const save = screen.getByRole('button', { name: 'Save split' })
    expect(save).toBeDisabled() // second part empty: not two valid parts yet
    expect(screen.getByRole('status')).toHaveTextContent('every part needs a non-zero amount')
    fireEvent.change(screen.getByLabelText('Amount for part 1'), { target: { value: '60.05' } })
    expect(screen.getByRole('status')).toHaveTextContent('40.05 left to allocate.')
    fireEvent.change(screen.getByLabelText('Amount for part 2'), { target: { value: '40.06' } })
    expect(screen.getByRole('status')).toHaveTextContent('Over-allocated by 0.01.')
    expect(save).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Amount for part 2'), { target: { value: '40.05' } })
    expect(screen.getByRole('status')).toHaveTextContent('Fully allocated.')
    expect(save).toBeEnabled()
    fireEvent.click(save)
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(body).toEqual({
      splits: [
        { category_id: tx.category_id ?? null, amount: '-60.05' },
        { category_id: null, amount: '-40.05' },
      ],
    })
  })

  it('shows the server error and stays open', async () => {
    server.use(
      http.put('/api/v1/transactions/:id/splits', () =>
        HttpResponse.json({ error: 'invalid splits: nope', code: 'INVALID_SPLITS' }, { status: 400 }),
      ),
    )
    const { onClose } = setup()
    fireEvent.change(screen.getByLabelText('Amount for part 1'), { target: { value: '50.00' } })
    fireEvent.change(screen.getByLabelText('Amount for part 2'), { target: { value: '50.10' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save split' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid splits: nope')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes on Escape', () => {
    const { onClose } = setup()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  it('explains a stale split after the bank changed the amount (#445)', () => {
    const stale = { ...tx, amount: '-100.50', splits_need_attention: true, splits: [
      { id: 1, transaction_id: tx.id, category_id: 1, amount: '-60.05' },
      { id: 2, transaction_id: tx.id, category_id: 2, amount: '-40.05' },
    ] }
    render(<SplitTransactionModal tx={stale} currency="USD" categories={fx.categories} onClose={vi.fn()} onSaved={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent('bank changed this amount')
    expect(screen.getByRole('button', { name: 'Save split' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('0.4 left to allocate.')
  })
})
