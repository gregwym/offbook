import { useEffect, useRef, useState } from 'react'
import type { Transaction } from '../types/transaction'
import { ModalShell } from './ModalShell'

type Props = {
  tx: Transaction
  // canHide: offer "Hide instead" (a flow row not already hidden or out of
  // spending as a transfer).
  canHide: boolean
  onClose: () => void
  onDelete: () => Promise<void>
  onHide: () => Promise<void>
}

// DeleteTransactionDialog confirms a transaction delete (#696). A row your
// bank synced can be sent again (a pending charge posting, a re-sync), and
// then it comes back into Needs review, so the dialog says that up front and
// offers Hide, which keeps the row in the balance but out of spending and
// budgets. Manual and imported rows get a plain confirm.
export function DeleteTransactionDialog({ tx, canHide, onClose, onDelete, onHide }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const synced = tx.source === 'plaid'
  const label = tx.merchant_name || tx.description || 'this transaction'

  useEffect(() => {
    cancelRef.current?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const run = async (action: () => Promise<void>, failure: string) => {
    setBusy(true)
    setError(null)
    try {
      await action()
      onClose()
    } catch {
      setError(failure)
      setBusy(false)
    }
  }

  const btn = 'min-h-10 rounded-md px-3 py-2 text-sm font-medium disabled:opacity-50'
  return (
    <ModalShell sheet size="md" aria-labelledby="delete-tx-title">
      <div className="px-5 pt-4">
        <h2 id="delete-tx-title" className="text-lg font-semibold text-gray-900">
          {synced ? 'Delete this bank transaction?' : 'Delete this transaction?'}
        </h2>
        <p className="mt-0.5 truncate text-sm text-gray-600">{label}</p>
      </div>
      <div className="space-y-2 px-5 py-3 text-sm text-gray-700">
        {synced ? (
          <>
            <p>
              Your bank may send it again{tx.pending ? ' when this pending charge posts' : ', for example on a later sync'}.
              If it does, it comes back so your balance stays right, and it shows up in <strong>Needs review</strong>.
            </p>
            {canHide && (
              <p>
                To keep it out of spending and budgets for good, hide it instead.
                Hidden rows still count toward the account balance.
              </p>
            )}
          </>
        ) : (
          <p>It is removed from the account and its balance.</p>
        )}
        {error && (
          <p role="alert" className="rounded border border-red-200 bg-red-50 px-2 py-1 text-red-700">{error}</p>
        )}
      </div>
      <div className="flex flex-col-reverse gap-2 border-t border-gray-200 px-5 py-3 sm:flex-row sm:justify-end">
        <button
          ref={cancelRef}
          type="button"
          onClick={onClose}
          disabled={busy}
          className={`${btn} border border-gray-300 text-gray-700 hover:bg-gray-50`}
        >
          Cancel
        </button>
        {synced && canHide && (
          <button
            type="button"
            onClick={() => void run(onHide, 'Could not hide the transaction. Try again.')}
            disabled={busy}
            className={`${btn} border border-indigo-300 text-indigo-700 hover:bg-indigo-50`}
          >
            Hide instead
          </button>
        )}
        <button
          type="button"
          onClick={() => void run(onDelete, 'Could not delete the transaction. Try again.')}
          disabled={busy}
          className={`${btn} bg-red-600 text-white hover:bg-red-700`}
        >
          Delete
        </button>
      </div>
    </ModalShell>
  )
}
