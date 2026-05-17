import { useEffect, useState } from 'react'
import { AmountDisplay } from '../components/AmountDisplay'
import { getDashboardSummary } from '../api/dashboard'
import { DASHBOARD_PERIODS, type DashboardPeriod, type DashboardSummary } from '../types/dashboard'

const PERIOD_LABELS: Record<DashboardPeriod, string> = {
  current_month: 'Current month',
  last_30d: 'Last 30 days',
  ytd: 'Year to date',
}

export function DashboardPage() {
  const [period, setPeriod] = useState<DashboardPeriod>('current_month')
  const [summary, setSummary] = useState<DashboardSummary | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const run = async () => {
      setLoading(true)
      setError(null)
      try {
        const s = await getDashboardSummary(period)
        if (!cancelled) setSummary(s)
      } catch (err) {
        if (!cancelled) setError(errMsg(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void run()
    return () => { cancelled = true }
  }, [period])

  return (
    <div>
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-gray-900">Dashboard</h1>
        <select
          className="rounded border border-gray-300 px-2 py-1 text-sm"
          value={period}
          onChange={(e) => setPeriod(e.target.value as DashboardPeriod)}
        >
          {DASHBOARD_PERIODS.map((p) => (
            <option key={p} value={p}>{PERIOD_LABELS[p]}</option>
          ))}
        </select>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-3">
        <SummaryCard label="Net worth">
          <AmountDisplay amount={summary?.net_worth} />
        </SummaryCard>
        <SummaryCard label="Income (period)">
          <AmountDisplay amount={summary?.income} />
        </SummaryCard>
        <SummaryCard label="Spending (period)">
          <AmountDisplay amount={summary?.spending} />
        </SummaryCard>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
        <SummaryCard label="Accounts">
          <span className="text-2xl font-semibold text-gray-900">{summary?.account_count ?? '—'}</span>
        </SummaryCard>
        <SummaryCard label="Transactions (period)">
          <span className="text-2xl font-semibold text-gray-900">{summary?.transaction_count ?? '—'}</span>
        </SummaryCard>
      </div>

      <div className="mt-6 rounded-lg border border-gray-200 bg-white p-4">
        <h2 className="mb-3 text-sm font-medium uppercase tracking-wider text-gray-500">Spending by category</h2>
        {loading && !summary && <div className="text-sm text-gray-400">Loading…</div>}
        {summary && summary.by_category.length === 0 && (
          <div className="text-sm text-gray-400">No transactions in this period.</div>
        )}
        {summary && summary.by_category.length > 0 && (
          <ul className="divide-y divide-gray-100 text-sm">
            {summary.by_category.map((row) => (
              <li key={`${row.category_id ?? 'null'}`} className="flex items-center justify-between py-2">
                <span className="text-gray-700">{row.name}</span>
                <AmountDisplay amount={row.amount} signed />
              </li>
            ))}
          </ul>
        )}
      </div>

      {summary && (
        <p className="mt-3 text-xs text-gray-400">
          {summary.period.from.slice(0, 10)} → {summary.period.to.slice(0, 10)}
        </p>
      )}
    </div>
  )
}

function SummaryCard({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="text-xs font-medium uppercase tracking-wider text-gray-500">{label}</div>
      <div className="mt-2 text-2xl font-semibold text-gray-900">{children}</div>
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
