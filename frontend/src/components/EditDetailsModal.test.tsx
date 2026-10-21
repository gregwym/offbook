import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import * as fx from '../test/fixtures'
import { EditDetailsModal } from './EditDetailsModal'

const tx = { ...fx.transaction1, source: 'plaid' as const, merchant_name: 'SQ *BLUE BOTTLE', notes: null }

function setup() {
  const onSaved = vi.fn()
  const onClose = vi.fn()
  render(<EditDetailsModal tx={tx} currency="USD" onClose={onClose} onSaved={onSaved} />)
  return { onSaved, onClose }
}

function capturePatch() {
  const bodies: unknown[] = []
  server.use(
    http.patch('/api/v1/transactions/:id', async ({ request }) => {
      bodies.push(await request.json())
      return HttpResponse.json({ data: tx })
    }),
  )
  return bodies
}

describe('EditDetailsModal (#569)', () => {
  it('shows the bank description and saves a fixed merchant and a note', async () => {
    const bodies = capturePatch()
    const { onSaved, onClose } = setup()
    expect(screen.getByRole('dialog', { name: 'Edit details' })).toBeInTheDocument()
    expect(screen.getByText('Bank description')).toBeInTheDocument()
    expect(screen.getByText('WHOLEFDS MKT')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Merchant'), { target: { value: '  Blue Bottle Coffee ' } })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Coffee with Sam' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(onClose).toHaveBeenCalled()
    expect(bodies).toEqual([{ merchant_name: 'Blue Bottle Coffee', notes: 'Coffee with Sam' }])
  })

  it('saving only a note leaves the bank merchant unpinned', async () => {
    const bodies = capturePatch()
    const { onSaved } = setup()
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'reimbursable' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(bodies).toEqual([{ notes: 'reimbursable' }])
  })

  it('clearing the merchant sends a blank so the bank name comes back', async () => {
    const bodies = capturePatch()
    const { onSaved } = setup()
    fireEvent.change(screen.getByLabelText('Merchant'), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(bodies).toEqual([{ merchant_name: '' }])
  })

  it('closes without a request when nothing changed', () => {
    const bodies = capturePatch()
    const { onSaved, onClose } = setup()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onClose).toHaveBeenCalled()
    expect(onSaved).not.toHaveBeenCalled()
    expect(bodies).toEqual([])
  })

  it('keeps the dialog open with the server error when saving fails', async () => {
    server.use(
      http.patch('/api/v1/transactions/:id', () =>
        HttpResponse.json({ error: 'transaction not found', code: 'TRANSACTION_NOT_FOUND' }, { status: 404 }),
      ),
    )
    const { onSaved, onClose } = setup()
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('transaction not found')
    expect(onSaved).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })
})
