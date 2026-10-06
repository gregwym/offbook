// ReconciliationPage — per-account reconciliation view (#370). Surfaces the
// ADR-0017 engine (observations → fold-vs-reported → opening_balance/
// adjustment rows) that otherwise has no UI: every reconciling row is tied
// to the observation that caused it, and an unexplained adjustment can be
// acknowledged (reviewed, with an optional note) without touching the
// ledger. Linked from the Accounts list "needs attention" flag and from
// adjustment/opening-balance rows in the Transactions list.
import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, CheckCircle2 } from 'lucide-react'
import { getAccount } from '../api/accounts'
import { acknowledgeAdjustment, getReconciliation } from '../api/reconciliation'
import { AmountDisplay } from '../components/AmountDisplay'
import { TimeAgo } from '../components/TimeAgo'
import type { Account } from '../types/account'
import type { ReconciliationCheckpoint, ReconciliationReport } from '../types/reconciliation'

export function ReconciliationPage() {
  const { id } = useParams<{ id: string }>()
  const accountId = Number(id)
  const validID = Number.isFinite(accountId)
  const [account, setAccount] = useState<Account | null>(null)
  const [report, setReport] = useState<ReconciliationReport | null>(null)
  const [loading, setLoading] = useState(validID)
  const [error, setError] = useState<string | null>(validID ? null : 'Invalid account id.')
  const [acknowledging, setAcknowledging] = useState<number | null>(null)
  const [note, setNote] = useState('')

  // setState only fires inside the then/catch/finally callbacks — the effect
  // body itself stays sync-pure, satisfying react-hooks/set-state-in-effect
  // (see SettingsPage's `refresh` for the same pattern).
  const load = useCallback(() => {
    return Promise.all([getAccount(accountId), getReconciliation(accountId)])
      .then(([acct, rep]) => {
        setAccount(acct)
        setReport(rep)
        setError(null)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : 'Failed to load reconciliation data.')
      })
      .finally(() => setLoading(false))
  }, [accountId])

  useEffect(() => {
    if (validID) void load()
  }, [validID, load])

  const submitAcknowledge = async (txnId: number) => {
    try {
      await acknowledgeAdjustment(txnId, note.trim() || undefined)
      setAcknowledging(null)
      setNote('')
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to acknowledge adjustment.')
    }
  }

  return (
    <div>
      <Link to="/accounts" className="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700">
        <ArrowLeft size={14} /> Back to Accounts
      </Link>
      <div className="mt-2 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">
            Reconciliation{account ? `: ${account.name}` : ''}
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Every observed balance, the ledger total right before it, and the opening_balance/
            adjustment row (if any) that absorbed the difference.
          </p>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {loading && <p className="mt-6 text-sm text-gray-500">Loading…</p>}

      {!loading && report && (
        <div className="mt-6 overflow-x-auto rounded-lg border border-gray-200 bg-white">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
              <tr>
                <th className="px-4 py-2 text-left">As of</th>
                <th className="px-4 py-2 text-left">Source</th>
                <th className="px-4 py-2 text-right">Reported</th>
                <th className="px-4 py-2 text-right">Ledger before</th>
                <th className="px-4 py-2 text-right">Delta</th>
                <th className="px-4 py-2 text-left">Row</th>
                <th className="px-4 py-2 text-left">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {report.checkpoints.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-4 py-6 text-center text-gray-400">
                    No observations recorded yet for this account.
                  </td>
                </tr>
              )}
              {report.checkpoints.map((cp) => (
                <CheckpointRow
                  key={cp.observation_id}
                  cp={cp}
                  acknowledging={acknowledging === cp.transaction_id}
                  note={note}
                  onNoteChange={setNote}
                  onStartAcknowledge={() => {
                    setAcknowledging(cp.transaction_id ?? null)
                    setNote('')
                  }}
                  onCancelAcknowledge={() => setAcknowledging(null)}
                  onSubmitAcknowledge={() => cp.transaction_id && void submitAcknowledge(cp.transaction_id)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function CheckpointRow({
  cp,
  acknowledging,
  note,
  onNoteChange,
  onStartAcknowledge,
  onCancelAcknowledge,
  onSubmitAcknowledge,
}: {
  cp: ReconciliationCheckpoint
  acknowledging: boolean
  note: string
  onNoteChange: (v: string) => void
  onStartAcknowledge: () => void
  onCancelAcknowledge: () => void
  onSubmitAcknowledge: () => void
}) {
  return (
    <>
      <tr className={cp.needs_attention ? 'bg-amber-50' : undefined}>
        <td className="whitespace-nowrap px-4 py-2">
          <TimeAgo when={cp.as_of} />
        </td>
        <td className="px-4 py-2 text-gray-500">{cp.source}</td>
        <td className="px-4 py-2 text-right">
          <AmountDisplay amount={cp.observed_quantity} fractionDigits={2} />
        </td>
        <td className="px-4 py-2 text-right text-gray-500">
          <AmountDisplay amount={cp.prior_fold} fractionDigits={2} />
        </td>
        <td className="px-4 py-2 text-right">
          <AmountDisplay amount={cp.delta} fractionDigits={2} signed />
        </td>
        <td className="px-4 py-2">
          {cp.kind ? (
            <span className="inline-flex items-center rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-xs font-medium text-gray-600">
              {cp.kind === 'opening_balance' ? 'Opening balance' : 'Adjustment'}
            </span>
          ) : (
            <span className="text-xs text-gray-400">matched, no row</span>
          )}
        </td>
        <td className="px-4 py-2">
          {cp.needs_attention && !acknowledging && (
            <button
              onClick={onStartAcknowledge}
              className="inline-flex items-center gap-1 rounded-md border border-amber-300 bg-amber-50 px-2 py-1 text-xs font-medium text-amber-800 hover:bg-amber-100"
            >
              <AlertTriangle size={12} /> Needs attention — Acknowledge
            </button>
          )}
          {!cp.needs_attention && cp.kind === 'adjustment' && cp.acknowledged && (
            <span
              title={cp.acknowledged_note ?? undefined}
              className="inline-flex items-center gap-1 text-xs text-emerald-700"
            >
              <CheckCircle2 size={12} /> Reviewed
              {cp.acknowledged_at && (
                <>
                  {' '}
                  <TimeAgo when={cp.acknowledged_at} />
                </>
              )}
            </span>
          )}
        </td>
      </tr>
      {acknowledging && (
        <tr className="bg-amber-50">
          <td colSpan={7} className="px-4 pb-3">
            <div className="flex items-center gap-2">
              <input
                type="text"
                value={note}
                onChange={(e) => onNoteChange(e.target.value)}
                placeholder="Optional note (e.g. confirmed with bank statement)"
                className="flex-1 rounded-md border border-gray-300 px-2 py-1 text-sm"
              />
              <button
                onClick={onSubmitAcknowledge}
                className="rounded-md bg-indigo-600 px-3 py-1 text-sm font-medium text-white hover:bg-indigo-700"
              >
                Acknowledge
              </button>
              <button
                onClick={onCancelAcknowledge}
                className="rounded-md border border-gray-300 px-3 py-1 text-sm text-gray-600 hover:bg-gray-50"
              >
                Cancel
              </button>
            </div>
          </td>
        </tr>
      )}
    </>
  )
}
