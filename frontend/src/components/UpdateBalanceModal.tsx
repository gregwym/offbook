// UpdateBalanceModal (#504): a manual account's "what does the bank say?"
// action. The user enters the current balance; the backend records it as a
// balance observation and reconciles the ledger to it with an
// opening_balance/adjustment row, which the Reconciliation view explains.
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { updateAccountBalance } from '../api/accounts'
import { AmountDisplay } from './AmountDisplay'
import { isLiabilityType, type Account, type UpdateBalanceResult } from '../types/account'
import { absDecimal } from '../lib/decimal'
import { entryFromLedger, ledgerFromEntry } from '../lib/liabilityEntry'
import { LiabilityReading } from './LiabilityReading'
import { ModalShell } from './ModalShell'

type Props = {
  account: Account
  onClose: () => void
  // Fires after a successful save so the host page can refetch.
  onUpdated?: (res: UpdateBalanceResult) => void
}

const DECIMAL = /^-?\d+(\.\d+)?$/

export function UpdateBalanceModal({ account, onClose, onUpdated }: Props) {
  // #667: a liability is entered as the amount owed, as the statement shows it.
  const [balance, setBalance] = useState(entryFromLedger(account.account_type, account.balance))
  const [asOf, setAsOf] = useState(todayISO())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<UpdateBalanceResult | null>(null)
  // A past-dated save (#669) is the balance on that day, not today's: the
  // confirmation shows both, since later transactions still apply.
  const [saved, setSaved] = useState<{ entered: string; asOf: string | null } | null>(null)
  const liability = isLiabilityType(account.account_type)

  const submit = async () => {
    setError(null)
    const entered = balance.replace(/[,\s$]/g, '')
    if (!DECIMAL.test(entered)) {
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
    const value = ledgerFromEntry(account.account_type, entered)
    setSubmitting(true)
    try {
      // Leave as_of off for today so the backend stamps the observation "now".
      const res = await updateAccountBalance(account.id, asOf === todayISO() ? { balance: value } : { balance: value, as_of: asOf })
      setSaved({ entered, asOf: asOf === todayISO() ? null : asOf })
      setResult(res)
      onUpdated?.(res)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ModalShell size="md" aria-labelledby="update-balance-title">
      <div id="update-balance-title" className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
        Update balance: {account.name}
      </div>
      {result ? (
        <>
          <div className="space-y-2 px-5 py-4 text-sm text-gray-700" role="status">
            {saved?.asOf && (
              <p>
                Saved{' '}
                <AmountDisplay amount={saved.entered} currency={account.currency} className="font-semibold" />
                {liability ? ' owed' : ''} as your balance on {formatDay(saved.asOf)}. Transactions after that date
                still count.
              </p>
            )}
            <p>
              {saved?.asOf
                ? !liability
                  ? 'Balance today is '
                  : inCredit(result.account.balance)
                    ? 'Today you’re in credit by '
                    : 'Today you owe '
                : !liability
                  ? 'Balance is now '
                  : inCredit(result.account.balance)
                    ? 'In credit by '
                    : 'You now owe '}
              <AmountDisplay
                amount={liability ? absDecimal(result.account.balance) : entryFromLedger(account.account_type, result.account.balance)}
                currency={account.currency}
                className="font-semibold"
              />
              .
            </p>
            {result.recorded ? (
              <p>
                <RecordedDifference
                  amount={result.recorded.amount}
                  currency={account.currency}
                  liability={liability}
                  day={saved?.asOf ? formatDay(saved.asOf) : null}
                />{' '}
                {/* #673: one name per row, the Reconciliation page's. The
                    +delta is a correction, never "an opening balance of". */}
                {result.recorded.kind === 'opening_balance'
                  ? 'The difference is recorded as this account’s opening balance, so it doesn’t count as spending or income.'
                  : 'The difference is recorded as an adjustment, so it doesn’t count as spending or income.'}
              </p>
            ) : (
              <p>
                Your transactions{saved?.asOf ? ` up to ${formatDay(saved.asOf)}` : ''} already add up to this balance,
                so nothing was adjusted.
              </p>
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
              {liability ? 'Enter what your statement says you owe.' : 'Enter the balance your bank shows.'} If your
              transactions don’t add up to it, Offbook records
              the difference as an adjustment you can review on the reconciliation page.
            </p>
            {error && (
              <div role="alert" className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                {error}
              </div>
            )}
            <Field label={`${liability ? 'Amount owed' : 'Balance'} (${account.currency})`}>
              <input
                className={inputClass}
                value={balance}
                onChange={(e) => {
                  setBalance(e.target.value)
                  // #512: a stale "enter a number" error must not sit next
                  // to a value that is now valid.
                  if (error && DECIMAL.test(e.target.value.replace(/[,\s$]/g, ''))) setError(null)
                }}
                inputMode="decimal"
                aria-describedby={liability ? 'update-balance-reading' : undefined}
                autoFocus
              />
            </Field>
            {liability && (
              <div className="-mt-2">
                <LiabilityReading id="update-balance-reading" entered={balance} currency={account.currency} />
              </div>
            )}
            <Field label="As of">
              <input
                type="date"
                className={inputClass}
                value={asOf}
                max={todayISO()}
                onChange={(e) => setAsOf(e.target.value)}
                aria-describedby={asOf && asOf < todayISO() ? 'update-balance-past' : undefined}
              />
            </Field>
            {asOf && asOf < todayISO() && (
              <p id="update-balance-past" className="-mt-2 text-xs text-gray-500">
                Offbook checks this against your transactions up to {formatDay(asOf)}. Transactions after that date
                stay as they are.
              </p>
            )}
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
    </ModalShell>
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

// inCredit: a stored liability balance above zero means the lender owes the user.
function inCredit(balance: string): boolean {
  return !balance.trim().startsWith('-') && absDecimal(balance) !== '0'
}

// RecordedDifference says what Offbook changed to match the reported balance,
// as a plain sentence (#673): "Offbook added 100.00 on Aug 30 so your
// transactions add up to it." For a card or loan the stored sign is the
// opposite of what's owed, so it speaks in amount owed.
function RecordedDifference({ amount, currency, liability, day }: {
  amount: string
  currency: string
  liability: boolean
  day: string | null
}) {
  const up = !amount.trim().startsWith('-')
  const value = <AmountDisplay amount={absDecimal(amount)} currency={currency} className="font-semibold" />
  const on = day ? ` on ${day}` : ''
  if (liability) {
    return up ? (
      <>Offbook took {value} off what you owe{on} so your transactions add up to it.</>
    ) : (
      <>Offbook added {value} to what you owe{on} so your transactions add up to it.</>
    )
  }
  return (
    <>
      Offbook {up ? 'added' : 'took off'} {value}
      {on} so your transactions add up to it.
    </>
  )
}

function todayISO(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${dd}`
}

// formatDay renders a YYYY-MM-DD date for copy, e.g. "May 31, 2026".
function formatDay(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
