import { useEffect, useState } from 'react'
import { updateTransaction } from '../api/transactions'
import { AmountDisplay } from './AmountDisplay'
import { DateDisplay } from './DateDisplay'
import type { Transaction, UpdateTransactionInput } from '../types/transaction'
import { ModalShell } from './ModalShell'

type Props = {
  tx: Transaction
  currency: string
  onClose: () => void
  onSaved: () => void
}

export const MAX_MERCHANT_LENGTH = 200
export const MAX_NOTES_LENGTH = 1000

// EditDetailsModal fixes one transaction's merchant name and note (#569). A
// merchant the user types is kept when the bank syncs again or a rule renames
// (the backend pins it); a blank merchant hands the name back to the bank.
// Amount, date and category are not edited here — money never changes.
export function EditDetailsModal({ tx, currency, onClose, onSaved }: Props) {
  const [merchant, setMerchant] = useState(tx.merchant_name ?? '')
  const [notes, setNotes] = useState(tx.notes ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const fromBank = tx.source === 'plaid' || tx.source === 'csv' || tx.source === 'pdf'

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const save = async () => {
    // Send only what changed: re-sending an untouched bank merchant would pin
    // it. Blank (not null — null means "leave alone") clears a field.
    const patch: UpdateTransactionInput = {}
    if (merchant.trim() !== (tx.merchant_name ?? '').trim()) patch.merchant_name = merchant.trim()
    if (notes.trim() !== (tx.notes ?? '').trim()) patch.notes = notes.trim()
    if (Object.keys(patch).length === 0) {
      onClose()
      return
    }
    setSaving(true)
    setError(null)
    try {
      await updateTransaction(tx.id, patch)
      onSaved()
      onClose()
    } catch (err) {
      const r = (err as { response?: { data?: { error?: string } } }).response
      setError(r?.data?.error ?? 'Could not save. Check your connection and try again.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <ModalShell sheet aria-labelledby="edit-details-title">
      <form className="contents" onSubmit={(e) => { e.preventDefault(); void save() }}>
        <div className="border-b border-gray-200 px-5 py-3">
          <div id="edit-details-title" className="text-lg font-semibold text-gray-900">Edit details</div>
          <div className="text-sm text-gray-600">
            <DateDisplay value={tx.transaction_date} /> · <AmountDisplay amount={tx.amount} currency={currency} signed />
          </div>
        </div>
        <div className="space-y-4 px-5 py-4">
          {tx.description && (
            <div>
              <div className="text-xs font-medium text-gray-500">{fromBank ? 'Bank description' : 'Description'}</div>
              <div className="break-words font-mono text-sm text-gray-800">{tx.description}</div>
            </div>
          )}
          <div>
            <label htmlFor="edit-details-merchant" className="text-sm font-medium text-gray-700">Merchant</label>
            <input
              id="edit-details-merchant"
              aria-describedby="edit-details-merchant-hint"
              value={merchant}
              onChange={(e) => setMerchant(e.target.value)}
              maxLength={MAX_MERCHANT_LENGTH}
              placeholder={fromBank ? 'Use the bank’s name' : 'e.g. Blue Bottle Coffee'}
              className="mt-1 block min-h-10 w-full rounded border border-gray-300 px-2 text-sm"
            />
            <p id="edit-details-merchant-hint" className="mt-1 text-xs text-gray-500">
              {fromBank
                ? 'Your name stays when the bank syncs again and isn’t changed by rules. Clear it to go back to the bank’s name.'
                : 'Shown in the list and in Top merchants. Rules won’t rename it.'}
            </p>
          </div>
          <div>
            <label htmlFor="edit-details-notes" className="text-sm font-medium text-gray-700">Notes</label>
            <textarea
              id="edit-details-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              maxLength={MAX_NOTES_LENGTH}
              rows={3}
              placeholder="Add a note for yourself"
              className="mt-1 block w-full rounded border border-gray-300 px-2 py-1.5 text-sm"
            />
          </div>
          <p className="text-xs text-gray-500">Amount, date and balance don’t change.</p>
          {error && <div role="alert" className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="min-h-10 rounded border border-gray-300 px-4 text-sm">Cancel</button>
          <button
            type="submit"
            disabled={saving}
            className="min-h-10 rounded bg-indigo-600 px-4 text-sm font-medium text-white disabled:opacity-40"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </ModalShell>
  )
}
