import { useState } from 'react'
import { ArrowRightLeft, X } from 'lucide-react'
import { AmountDisplay } from './AmountDisplay'
import { DateDisplay } from './DateDisplay'
import { confirmTransferMatch } from '../api/transfers'
import type { TransferCandidate } from '../api/transfers'
import type { Account } from '../types/account'
import type { Asset } from '../types/asset'

type Props = {
  candidates: TransferCandidate[]
  accountByID: Map<number, Account>
  assetByID: Map<number, Asset>
  onClose: () => void
  // Called after each successful confirm so the page can refresh its
  // transaction list + candidate count.
  onConfirmed: () => void
}

// TransferMatchesModal is the #371 review queue: every pair the deterministic
// matcher proposed, confirmed one at a time. Nothing links until the user
// clicks "Confirm match" on that specific pair.
export function TransferMatchesModal({ candidates, accountByID, assetByID, onClose, onConfirmed }: Props) {
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [confirmedKeys, setConfirmedKeys] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  const keyOf = (c: TransferCandidate) => `${c.transaction_a.id}-${c.transaction_b.id}`

  const confirm = async (c: TransferCandidate) => {
    const key = keyOf(c)
    setBusyKey(key)
    setError(null)
    try {
      await confirmTransferMatch(c.transaction_a.id, c.transaction_b.id)
      setConfirmedKeys((prev) => new Set(prev).add(key))
      onConfirmed()
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setBusyKey(null)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-2xl rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <div className="flex items-center gap-2 text-lg font-semibold text-gray-900">
            <ArrowRightLeft size={18} /> Possible transfers
          </div>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-700">
            <X size={18} />
          </button>
        </div>
        <div className="max-h-[70vh] overflow-y-auto px-5 py-4">
          <p className="mb-3 text-sm text-gray-500">
            These look like money moving between your own accounts. Confirm a match to stop it counting as income or
            spending.
          </p>
          {error && (
            <div className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
          )}
          {candidates.length === 0 && (
            <div className="py-6 text-center text-sm text-gray-400">No possible transfers found.</div>
          )}
          <ul className="divide-y divide-gray-100">
            {candidates.map((c) => {
              const key = keyOf(c)
              const confirmed = confirmedKeys.has(key)
              const accA = accountByID.get(c.transaction_a.account_id)
              const accB = accountByID.get(c.transaction_b.account_id)
              const currencyA = assetByID.get(c.transaction_a.asset_id)?.symbol ?? accA?.currency ?? 'USD'
              const currencyB = assetByID.get(c.transaction_b.asset_id)?.symbol ?? accB?.currency ?? 'USD'
              return (
                <li key={key} className="flex items-center justify-between gap-3 py-3">
                  <div className="flex-1 text-sm">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-gray-900">{accA?.name ?? `#${c.transaction_a.account_id}`}</span>
                      <AmountDisplay amount={c.transaction_a.amount} currency={currencyA} signed />
                      <span className="text-gray-400">
                        <DateDisplay value={c.transaction_a.transaction_date} />
                      </span>
                    </div>
                    <div className="mt-1 flex items-center gap-2">
                      <span className="font-medium text-gray-900">{accB?.name ?? `#${c.transaction_b.account_id}`}</span>
                      <AmountDisplay amount={c.transaction_b.amount} currency={currencyB} signed />
                      <span className="text-gray-400">
                        <DateDisplay value={c.transaction_b.transaction_date} />
                      </span>
                    </div>
                  </div>
                  <button
                    type="button"
                    disabled={busyKey === key || confirmed}
                    onClick={() => confirm(c)}
                    className="shrink-0 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
                  >
                    {confirmed ? 'Matched' : busyKey === key ? 'Confirming…' : 'Confirm match'}
                  </button>
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
