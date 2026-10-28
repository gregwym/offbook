// ReconciliationPage — per-account reconciliation view (#370). Surfaces the
// ADR-0017 engine (observations → fold-vs-reported → opening_balance/
// adjustment rows) in plain language (#512) that otherwise has no UI: every reconciling row is tied
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
import { sourceLabel } from '../lib/balanceSource'
import { TimeAgo } from '../components/TimeAgo'
import { useMinWidth } from '../hooks/useMinWidth'
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
  const wide = useMinWidth(768)

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

  const ackProps = (cp: ReconciliationCheckpoint) => ({
    cp,
    acknowledging: cp.transaction_id != null && acknowledging === cp.transaction_id,
    note,
    onNoteChange: setNote,
    onStartAcknowledge: () => {
      setAcknowledging(cp.transaction_id ?? null)
      setNote('')
    },
    onCancelAcknowledge: () => setAcknowledging(null),
    onSubmitAcknowledge: () => cp.transaction_id && void submitAcknowledge(cp.transaction_id),
  })

  return (
    <div>
      <Link to="/accounts" className="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-gray-700">
        <ArrowLeft size={14} /> Back to Accounts
      </Link>
      <div className="mt-2">
        <h1 className="text-2xl font-semibold text-gray-900">
          Reconciliation{account ? `: ${account.name}` : ''}
        </h1>
        <p className="mt-1 text-sm text-gray-500">{RECONCILIATION_INTRO}</p>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {loading && <p className="mt-6 text-sm text-gray-500">Loading…</p>}

      {!loading && report && report.checkpoints.length === 0 && (
        <p className="mt-6 rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-sm text-gray-500">
          No balances recorded yet for this account. Use “Update balance” on the Accounts page to enter what your bank
          shows.
        </p>
      )}

      {!loading && report && report.checkpoints.length > 0 && wide && (
        <div className="mt-6 rounded-lg border border-gray-200 bg-white">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
              <tr>
                <th className="px-4 py-2 text-left">Date</th>
                <th className="px-4 py-2 text-left">Reported by</th>
                <th className="px-4 py-2 text-right">Reported balance</th>
                <th className="px-4 py-2 text-right">Your transactions</th>
                <th className="px-4 py-2 text-right">Difference</th>
                <th className="px-4 py-2 text-left">Recorded as</th>
                <th className="px-4 py-2 text-left">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {report.checkpoints.map((cp) => (
                <CheckpointRow key={cp.observation_id} {...ackProps(cp)} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Narrow screens (#512): one stacked card per observation, so the
          dates, amounts and the acknowledge form are all on screen without
          scrolling sideways. */}
      {!loading && report && report.checkpoints.length > 0 && !wide && (
        <ul className="mt-6 space-y-3" aria-label="Recorded balances">
          {report.checkpoints.map((cp) => (
            <CheckpointCard key={cp.observation_id} {...ackProps(cp)} />
          ))}
        </ul>
      )}
    </div>
  )
}

const RECONCILIATION_INTRO =
  'Each time your bank, a statement or you report a balance, Offbook compares it with what your transactions add up ' +
  'to. Any difference is recorded as an adjustment so the balance matches. Adjustments don’t count as spending or ' +
  'income; a large one is flagged so you can check it.'

function recordedAsLabel(kind: string | undefined): string {
  if (kind === 'opening_balance') return 'Opening balance'
  if (kind === 'adjustment') return 'Adjustment'
  return 'Nothing — it matched'
}

type CheckpointProps = {
  cp: ReconciliationCheckpoint
  acknowledging: boolean
  note: string
  onNoteChange: (v: string) => void
  onStartAcknowledge: () => void
  onCancelAcknowledge: () => void
  onSubmitAcknowledge: () => void
}

function CheckpointRow(props: CheckpointProps) {
  const { cp, acknowledging } = props
  return (
    <>
      <tr className={cp.needs_attention ? 'bg-amber-50' : undefined}>
        <td className="whitespace-nowrap px-4 py-2">
          <TimeAgo when={cp.as_of} />
        </td>
        <td className="whitespace-nowrap px-4 py-2 text-gray-500">{sourceLabel(cp.source)}</td>
        <td className="whitespace-nowrap px-4 py-2 text-right">
          <AmountDisplay amount={cp.observed_quantity} fractionDigits={2} />
        </td>
        <td className="whitespace-nowrap px-4 py-2 text-right text-gray-500">
          <AmountDisplay amount={cp.prior_fold} fractionDigits={2} />
        </td>
        <td className="whitespace-nowrap px-4 py-2 text-right">
          <AmountDisplay amount={cp.delta} fractionDigits={2} signed />
        </td>
        <td className="px-4 py-2">
          <KindChip kind={cp.kind} />
        </td>
        <td className="px-4 py-2">
          <Status {...props} />
        </td>
      </tr>
      {acknowledging && (
        <tr className="bg-amber-50">
          <td colSpan={7} className="px-4 pb-3">
            <AcknowledgeForm {...props} />
          </td>
        </tr>
      )}
    </>
  )
}

function CheckpointCard(props: CheckpointProps) {
  const { cp, acknowledging } = props
  return (
    <li
      className={`rounded-lg border px-4 py-3 text-sm ${cp.needs_attention ? 'border-amber-300 bg-amber-50' : 'border-gray-200 bg-white'}`}
    >
      <div className="flex items-center justify-between gap-2 text-xs text-gray-500">
        <TimeAgo when={cp.as_of} />
        <KindChip kind={cp.kind} />
      </div>
      <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-3 gap-y-1">
        <dt className="text-gray-600">{sourceLabel(cp.source)} reported</dt>
        <dd className="text-right font-medium text-gray-900">
          <AmountDisplay amount={cp.observed_quantity} fractionDigits={2} />
        </dd>
        <dt className="text-gray-600">Your transactions added up to</dt>
        <dd className="text-right text-gray-700">
          <AmountDisplay amount={cp.prior_fold} fractionDigits={2} />
        </dd>
        <dt className="text-gray-600">Difference</dt>
        <dd className="text-right">
          <AmountDisplay amount={cp.delta} fractionDigits={2} signed />
        </dd>
      </dl>
      <p className="mt-2 text-xs text-gray-500">{explain(cp)}</p>
      {(cp.needs_attention || cp.acknowledged) && (
        <div className="mt-2">
          <Status {...props} />
        </div>
      )}
      {acknowledging && (
        <div className="mt-2">
          <AcknowledgeForm {...props} />
        </div>
      )}
    </li>
  )
}

// explain is the card's one-line plain-language reading of a checkpoint.
function explain(cp: ReconciliationCheckpoint): string {
  if (cp.kind === 'opening_balance') return 'The difference was recorded as this account’s opening balance.'
  if (cp.kind === 'adjustment') return 'The difference was recorded as an adjustment.'
  return 'Your transactions matched, so nothing was adjusted.'
}

function KindChip({ kind }: { kind?: string }) {
  if (!kind) return <span className="text-xs text-gray-400">{recordedAsLabel(kind)}</span>
  return (
    <span className="inline-flex items-center whitespace-nowrap rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-xs font-medium text-gray-600">
      {recordedAsLabel(kind)}
    </span>
  )
}

function Status({ cp, acknowledging, onStartAcknowledge }: CheckpointProps) {
  if (cp.needs_attention && !acknowledging) {
    return (
      <button
        onClick={onStartAcknowledge}
        className="inline-flex min-h-9 items-center gap-1 rounded-md border border-amber-300 bg-amber-50 px-2 py-1 text-left text-xs font-medium text-amber-800 hover:bg-amber-100"
      >
        <AlertTriangle size={12} className="shrink-0" /> Large difference — mark as reviewed
      </button>
    )
  }
  if (!cp.needs_attention && cp.kind === 'adjustment' && cp.acknowledged) {
    return (
      <span title={cp.acknowledged_note ?? undefined} className="inline-flex items-center gap-1 whitespace-nowrap text-xs text-emerald-700">
        <CheckCircle2 size={12} /> Reviewed
        {cp.acknowledged_at && (
          <>
            {' '}
            <TimeAgo when={cp.acknowledged_at} />
          </>
        )}
      </span>
    )
  }
  return null
}

function AcknowledgeForm({ note, onNoteChange, onSubmitAcknowledge, onCancelAcknowledge }: CheckpointProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <input
        type="text"
        value={note}
        onChange={(e) => onNoteChange(e.target.value)}
        aria-label="Note"
        placeholder="Optional note, e.g. bank fee"
        className="min-w-0 flex-1 basis-full rounded-md border border-gray-300 px-2 py-1 text-sm sm:basis-auto"
      />
      <button
        onClick={onSubmitAcknowledge}
        className="rounded-md bg-indigo-600 px-3 py-1 text-sm font-medium text-white hover:bg-indigo-700"
      >
        Mark reviewed
      </button>
      <button
        onClick={onCancelAcknowledge}
        className="rounded-md border border-gray-300 px-3 py-1 text-sm text-gray-600 hover:bg-gray-50"
      >
        Cancel
      </button>
    </div>
  )
}
