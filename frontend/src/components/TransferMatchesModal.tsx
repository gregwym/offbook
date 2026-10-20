import { useState } from 'react'
import { ArrowRightLeft, X } from 'lucide-react'
import { AmountDisplay } from './AmountDisplay'
import { DateDisplay } from './DateDisplay'
import { confirmTransferMatch, dismissTransferMatch } from '../api/transfers'
import type { TransferCandidate } from '../api/transfers'
import type { Account } from '../types/account'
import type { Asset } from '../types/asset'

type Props = {
  candidates: TransferCandidate[]
  accountByID: Map<number, Account>
  assetByID: Map<number, Asset>
  onClose: () => void
  // Called after each successful confirm or dismiss so the page can refresh
  // its transaction list + candidate count.
  onConfirmed: () => void
}

type Decision = 'matched' | 'dismissed'

// TransferMatchesModal is the #371 review queue: every pair the deterministic
// matcher proposed, decided one at a time. Nothing links until the user
// clicks "Confirm match" on that specific pair; "Not a transfer" (#510)
// rejects a wrong guess so both rows count normally again.
export function TransferMatchesModal({ candidates, accountByID, assetByID, onClose, onConfirmed }: Props) {
  const [busy, setBusy] = useState<{ key: string; action: Decision } | null>(null)
  const [decided, setDecided] = useState<Map<string, Decision>>(new Map())
  const [error, setError] = useState<string | null>(null)
  const [status, setStatus] = useState<string | null>(null)

  const keyOf = (c: TransferCandidate) => `${c.transaction_a.id}-${c.transaction_b.id}`

  const decide = async (c: TransferCandidate, action: Decision) => {
    const key = keyOf(c)
    setBusy({ key, action })
    setError(null)
    setStatus(null)
    try {
      if (action === 'matched') {
        await confirmTransferMatch(c.transaction_a.id, c.transaction_b.id)
        setStatus('Matched as a transfer. It stays out of income and spending.')
      } else {
        await dismissTransferMatch(c.transaction_a.id, c.transaction_b.id)
        setStatus('Marked as not a transfer. Both transactions now count in income and spending.')
      }
      setDecided((prev) => new Map(prev).set(key, action))
      onConfirmed()
    } catch (err) {
      setError(`Couldn't save that decision: ${errMsg(err).replace(/\.$/, '')}. Try again.`)
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div role="dialog" aria-modal="true" aria-labelledby="transfer-matches-title" className="w-full max-w-2xl rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <h2 id="transfer-matches-title" className="flex items-center gap-2 text-lg font-semibold text-gray-900">
            <ArrowRightLeft size={18} /> Possible transfers
          </h2>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-700">
            <X size={18} />
          </button>
        </div>
        <div className="max-h-[70vh] overflow-y-auto px-5 py-4">
          <p className="mb-3 text-sm text-gray-500">
            These look like money moving between your own accounts, so they're left out of income and spending for
            now. Confirm the match if it's a transfer, or choose “Not a transfer” to count both normally.
          </p>
          {error && (
            <div role="alert" className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </div>
          )}
          <div aria-live="polite" className={status ? 'mb-3 rounded-md border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-800' : 'sr-only'}>
            {status}
          </div>
          {candidates.length === 0 && (
            <div className="py-6 text-center text-sm text-gray-400">No possible transfers left to review.</div>
          )}
          <ul className="divide-y divide-gray-100">
            {candidates.map((c) => {
              const key = keyOf(c)
              const outcome = decided.get(key)
              const rowBusy = busy?.key === key
              const accA = accountByID.get(c.transaction_a.account_id)
              const accB = accountByID.get(c.transaction_b.account_id)
              const nameA = accA?.name ?? `#${c.transaction_a.account_id}`
              const nameB = accB?.name ?? `#${c.transaction_b.account_id}`
              const currencyA = assetByID.get(c.transaction_a.asset_id)?.symbol ?? accA?.currency ?? 'USD'
              const currencyB = assetByID.get(c.transaction_b.asset_id)?.symbol ?? accB?.currency ?? 'USD'
              const pairLabel = `${nameA} and ${nameB}`
              return (
                <li key={key} className="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between">
                  <div className="min-w-0 flex-1 text-sm">
                    <div className="flex flex-wrap items-center gap-x-2">
                      <span className="font-medium text-gray-900">{nameA}</span>
                      <AmountDisplay amount={c.transaction_a.amount} currency={currencyA} signed />
                      <span className="text-gray-400">
                        <DateDisplay value={c.transaction_a.transaction_date} />
                      </span>
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-x-2">
                      <span className="font-medium text-gray-900">{nameB}</span>
                      <AmountDisplay amount={c.transaction_b.amount} currency={currencyB} signed />
                      <span className="text-gray-400">
                        <DateDisplay value={c.transaction_b.transaction_date} />
                      </span>
                    </div>
                  </div>
                  {outcome ? (
                    <span className="shrink-0 text-sm font-medium text-gray-500">
                      {outcome === 'matched' ? 'Matched' : 'Not a transfer'}
                    </span>
                  ) : (
                    <div className="flex shrink-0 gap-2">
                      <button
                        type="button"
                        disabled={rowBusy}
                        onClick={() => decide(c, 'dismissed')}
                        aria-label={`Not a transfer: ${pairLabel}`}
                        className="min-h-9 flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 sm:flex-none"
                      >
                        {rowBusy && busy?.action === 'dismissed' ? 'Saving…' : 'Not a transfer'}
                      </button>
                      <button
                        type="button"
                        disabled={rowBusy}
                        onClick={() => decide(c, 'matched')}
                        aria-label={`Confirm match: ${pairLabel}`}
                        className="min-h-9 flex-1 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50 sm:flex-none"
                      >
                        {rowBusy && busy?.action === 'matched' ? 'Confirming…' : 'Confirm match'}
                      </button>
                    </div>
                  )}
                </li>
              )
            })}
          </ul>
        </div>
        <div className="flex justify-end border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">
            Done
          </button>
        </div>
      </div>
    </div>
  )
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
