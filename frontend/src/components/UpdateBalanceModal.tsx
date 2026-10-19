// UpdateBalanceModal (#504): a manual account's "what does the bank say?"
// action. The user enters the current balance; the backend records it as a
// balance observation and reconciles the ledger to it with an
// opening_balance/adjustment row, which the Reconciliation view explains.
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { updateAccountBalance } from '../api/accounts'
import { AmountDisplay } from './AmountDisplay'
import type { Account, UpdateBalanceResult } from '../types/account'

type Props = {
  account: Account
  onClose: () => void
  // Fires after a successful save so the host page can refetch.
  onUpdated?: (res: UpdateBalanceResult) => void
}

const DECIMAL = /^-?\d+(\.\d+)?$/

export function UpdateBalanceModal({ account, onClose, onUpdated }: Props) {
  const [balance, setBalance] = useState(trimDecimal(account.balance))
  const [asOf, setAsOf] = useState(todayISO())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<UpdateBalanceResult | null>(null)
  const liability = account.account_type === 'credit_card' || account.account_type === 'loan'

  const submit = async () => {
    setError(null)
    const value = balance.replace(/[,\s$]/g, '')
    if (!DECIMAL.test(value)) {
      setError('Enter the balance as a number, e.g. 1234.56.')
      return
    }
    if (!asOf) {
      setError('Pick the date the balance is from.')
      return
    }
    if (asOf > todayISO()) {
      setError('The date can’t be in the future.')
      return
    }
    setSubmitting(true)
    try {
      const res = await updateAccountBalance(account.id, { balance: value, as_of: asOf })
      setResult(res)
      onUpdated?.(res)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div role="dialog" aria-modal="true" aria-labelledby="update-balance-title" className="w-full max-w-md rounded-lg bg-white shadow-xl">
        <div id="update-balance-title" className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          Update balance: {account.name}
        </div>
        {result ? (
          <>
            <div className="space-y-2 px-5 py-4 text-sm text-gray-700" role="status">
              <p>
                Balance is now{' '}
                <AmountDisplay amount={result.account.balance} currency={account.currency} className="font-semibold" />.
              </p>
              {result.recorded ? (
                <p>
                  Recorded{' '}
                  {result.recorded.kind === 'opening_balance' ? 'an opening balance of ' : 'an adjustment of '}
                  <AmountDisplay amount={result.recorded.amount} currency={account.currency} signed /> to match it.
                  Adjustments don’t count as spending or income.
                </p>
              ) : (
                <p>Your transactions already add up to this balance, so nothing was adjusted.</p>
              )}
              <Link
                to={`/accounts/${account.id}/reconciliation`}
                className="inline-block font-medium text-indigo-600 hover:text-indigo-800"
              >
                View reconciliation
              </Link>
            </div>
            <div className="flex justify-end border-t border-gray-200 px-5 py-3">
              <button
                type="button"
                onClick={onClose}
                className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700"
              >
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="space-y-3 px-5 py-4">
              <p className="text-xs text-gray-500">
                Enter the balance your bank shows. If your transactions don’t add up to it, Offbook records
                the difference as an adjustment you can review on the reconciliation page.
              </p>
              {error && (
                <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                  {error}
                </div>
              )}
              <Field label={`Balance (${account.currency})`}>
                <input
                  className={inputClass}
                  value={balance}
                  onChange={(e) => setBalance(e.target.value)}
                  inputMode="decimal"
                  autoFocus
                />
                {liability && (
                  <span className="mt-1 block text-[11px] text-gray-500">Enter what you owe as a negative number, e.g. -450.00.</span>
                )}
              </Field>
              <Field label="As of">
                <input type="date" className={inputClass} value={asOf} max={todayISO()} onChange={(e) => setAsOf(e.target.value)} />
              </Field>
            </div>
            <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
              <button type="button" onClick={onClose} disabled={submitting} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void submit()}
                disabled={submitting}
                className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                {submitting ? 'Saving…' : 'Save balance'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
  )
}

// trimDecimal turns the API's NUMERIC(30,18) string into an editable value
// ("1234.560000000000000000" -> "1234.56") without going through floats.
function trimDecimal(s: string): string {
  if (!s.includes('.')) return s
  const t = s.replace(/0+$/, '').replace(/\.$/, '')
  return t === '-0' ? '0' : t
}

function todayISO(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${dd}`
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
