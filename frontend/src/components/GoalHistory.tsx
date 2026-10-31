// GoalHistory — a goal's contribution history (#682): date, signed amount
// and the goal's total after each entry, newest first. Loads when mounted;
// the card re-mounts it after a contribution so it never shows a stale list.
import { useEffect, useState } from 'react'
import type { GoalContribution } from '../types/savingsGoal'
import { goalHistoryRows } from '../lib/goalHistory'
import { AmountDisplay } from './AmountDisplay'

type Props = {
  goalName: string
  currentAmount: string
  load: () => Promise<GoalContribution[]>
}

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; items: GoalContribution[] }

export function GoalHistory({ goalName, currentAmount, load }: Props) {
  const [state, setState] = useState<State>({ status: 'loading' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    load().then(
      (items) => { if (!cancelled) setState({ status: 'ready', items }) },
      (e: unknown) => { if (!cancelled) setState({ status: 'error', message: e instanceof Error ? e.message : 'request failed' }) },
    )
    return () => { cancelled = true }
  }, [load, attempt])

  if (state.status === 'loading') {
    return <p className="mt-2 text-xs text-gray-500" role="status">Loading history…</p>
  }
  if (state.status === 'error') {
    return (
      <div className="mt-2 flex items-center justify-between gap-2 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-800" role="alert">
        <span>Couldn’t load the history.</span>
        <button
          type="button"
          onClick={() => {
            setState({ status: 'loading' })
            setAttempt((n) => n + 1)
          }}
          className="font-medium underline hover:text-amber-900"
        >
          Try again
        </button>
      </div>
    )
  }

  const rows = goalHistoryRows(state.items, currentAmount)
  if (rows.length === 0) {
    return <p className="mt-2 text-xs text-gray-500">No contributions yet. Each one you log will appear here with the date.</p>
  }
  return (
    <ol aria-label={`Contribution history for ${goalName}`} className="mt-2 divide-y divide-gray-100 border-t border-gray-100 text-sm">
      {rows.map((r) =>
        r.kind === 'entry' ? (
          <li key={r.id} className="flex items-baseline justify-between gap-3 py-1.5">
            <span className="min-w-0">
              <span className="block text-gray-700">{r.amount.startsWith('-') ? 'Withdrawal' : 'Contribution'}</span>
              <span className="block text-xs text-gray-500">{formatDay(r.at)}</span>
            </span>
            <span className="shrink-0 text-right">
              <span className={['block font-medium', r.amount.startsWith('-') ? 'text-red-700' : 'text-emerald-700'].join(' ')}>
                {r.amount.startsWith('-') ? '−' : '+'}
                <AmountDisplay amount={r.amount.replace(/^-/, '')} currency="USD" />
              </span>
              <span className="block text-xs text-gray-500">
                Total <AmountDisplay amount={r.total} currency="USD" />
              </span>
            </span>
          </li>
        ) : (
          <li key="before" className="flex items-baseline justify-between gap-3 py-1.5 text-gray-500">
            <span className="text-xs">Saved before history was kept</span>
            <span className="shrink-0 text-xs">
              Total <AmountDisplay amount={r.total} currency="USD" />
            </span>
          </li>
        ),
      )}
    </ol>
  )
}

function formatDay(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}
