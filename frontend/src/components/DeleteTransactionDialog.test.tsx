import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import * as fx from '../test/fixtures'
import type { Transaction } from '../types/transaction'
import { DeleteTransactionDialog } from './DeleteTransactionDialog'

const synced: Transaction = { ...fx.transaction1, source: 'plaid', plaid_transaction_id: 'ptx-1', pending: true }

function setup(tx: Transaction = synced, canHide = true) {
  const onClose = vi.fn()
  const onDelete = vi.fn().mockResolvedValue(undefined)
  const onHide = vi.fn().mockResolvedValue(undefined)
  render(<DeleteTransactionDialog tx={tx} canHide={canHide} onClose={onClose} onDelete={onDelete} onHide={onHide} />)
  return { onClose, onDelete, onHide }
}

describe('DeleteTransactionDialog (#696)', () => {
  it('warns that the bank can send a synced row again and offers Hide', async () => {
    const { onHide, onDelete, onClose } = setup()
    expect(screen.getByRole('dialog', { name: 'Delete this bank transaction?' })).toBeInTheDocument()
    expect(screen.getByText(/when this pending charge posts/)).toBeInTheDocument()
    expect(screen.getByText(/Needs review/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: 'Hide instead' }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(onHide).toHaveBeenCalledOnce()
    expect(onDelete).not.toHaveBeenCalled()
  })

  it('keeps a plain confirm for a manual row', async () => {
    const { onDelete, onClose } = setup(fx.transaction1)
    expect(screen.getByRole('dialog', { name: 'Delete this transaction?' })).toBeInTheDocument()
    expect(screen.queryByText(/bank may send/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Hide instead' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(onDelete).toHaveBeenCalledOnce()
  })

  it('omits Hide when the row cannot be hidden, and shows a retryable error', async () => {
    const onClose = vi.fn()
    const onDelete = vi.fn().mockRejectedValueOnce(new Error('boom'))
    render(<DeleteTransactionDialog tx={synced} canHide={false} onClose={onClose} onDelete={onDelete} onHide={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Hide instead' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not delete')
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeEnabled()
  })

  it('closes on Escape', () => {
    const { onClose } = setup()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })
})
