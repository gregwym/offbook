import { useEffect } from 'react'
import { Activity, AlertCircle, Wallet } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { useHouseholdDashboardStore } from '../store/householdDashboardStore'
import { useScopeStore } from '../store/scopeStore'
import type { HouseholdPeriodKey } from '../types/householdAggregator'

const PERIODS: Array<{ key: HouseholdPeriodKey; label: string }> = [
  { key: 'current_month', label: 'This month' },
  { key: 'last_30d', label: 'Last 30 days' },
  { key: 'ytd', label: 'Year to date' },
]

export function HouseholdDashboardPage() {
  const { householdId } = useScopeStore()
  const { dashboard, period, loading, error, load, setPeriod, clearError } =
    useHouseholdDashboardStore()

  useEffect(() => {
    if (householdId != null) void load()
  }, [householdId, load])

  if (householdId == null) {
    return (
      <div className="rounded-lg border border-gray-200 bg-white p-8 text-center">
        <Wallet size={28} className="mx-auto text-gray-300 mb-2" />
        <h1 className="text-base font-medium text-gray-900">No household yet</h1>
        <p className="text-sm text-gray-500 mt-1">
          Use the scope switcher in the sidebar to create or join a household.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Household</h1>
          <p className="mt-1 text-sm text-gray-500">
            Aggregates over accounts members opted in. PII never leaves each member's book.
          </p>
        </div>
        <div className="inline-flex rounded-md border border-gray-200 bg-gray-50 p-0.5 text-xs">
          {PERIODS.map((p) => (
            <button
              key={p.key}
              type="button"
              onClick={() => void setPeriod(p.key)}
              className={[
                'px-2.5 py-1 rounded',
                period === p.key ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500 hover:text-gray-700',
              ].join(' ')}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      {loading && !dashboard && (
        <div className="rounded-lg border border-gray-200 bg-white px-5 py-8 text-center text-sm text-gray-400">
          Loading…
        </div>
      )}

      {!loading && !dashboard && !error && (
        <EmptyState />
      )}

      {dashboard && <DashboardBody />}
    </div>
  )
}

function DashboardBody() {
  const { dashboard } = useHouseholdDashboardStore()
  if (!dashboard) return null

  const hasShares = dashboard.account_count > 0

  return (
    <>
      {!hasShares && <ShareCTA />}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatCard label="Net worth">
          <AmountDisplay amount={dashboard.net_worth} />
        </StatCard>
        <StatCard label="Income (period)">
          <AmountDisplay amount={dashboard.income} />
        </StatCard>
        <StatCard label="Spending (period)">
          <AmountDisplay amount={dashboard.spending} />
        </StatCard>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <SmallStat label="Shared accounts" value={dashboard.account_count.toString()} />
        <SmallStat label="Transactions (period)" value={dashboard.transaction_count.toString()} />
        <SmallStat
          label="Members"
          value={
            dashboard.in_grace_count > 0
              ? `${dashboard.live_member_count} active · ${dashboard.in_grace_count} in grace`
              : `${dashboard.live_member_count} active`
          }
        />
      </div>

      {dashboard.in_grace_count > 0 && (
        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 flex items-start gap-2">
          <AlertCircle size={14} className="mt-0.5 shrink-0" />
          <span>
            {dashboard.in_grace_count} member{dashboard.in_grace_count === 1 ? '' : 's'} in grace —
            their accounts no longer feed live aggregates but historical totals still include their
            contributions.
          </span>
        </div>
      )}

      <section className="rounded-lg border border-gray-200 bg-white">
        <header className="flex items-center gap-2 border-b border-gray-200 px-5 py-3">
          <Activity size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">Spend by category</h2>
        </header>
        {dashboard.by_category.length === 0 ? (
          <div className="px-5 py-6 text-center text-sm text-gray-400">
            No spending in this period yet.
          </div>
        ) : (
          <div className="divide-y divide-gray-100">
            {dashboard.by_category.map((row) => (
              <div key={`${row.category_id ?? 'null'}-${row.name}`} className="flex items-center justify-between px-5 py-2.5">
                <span className="text-sm text-gray-900">{row.name}</span>
                <AmountDisplay amount={row.amount} className="text-sm font-medium text-gray-900" />
              </div>
            ))}
          </div>
        )}
      </section>

      <p className="text-xs text-gray-500">
        Per-member contribution tiles will land once the aggregator returns the breakdown — tracked
        in #149.
      </p>
    </>
  )
}

function ShareCTA() {
  return (
    <div className="rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-xs text-indigo-800">
      No shared accounts yet — visit Accounts (personal scope) and set an account's visibility to
      see it appear here.
    </div>
  )
}

function EmptyState() {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-8 text-center">
      <Wallet size={28} className="mx-auto text-gray-300 mb-2" />
      <h2 className="text-base font-medium text-gray-900">Nothing to aggregate yet</h2>
      <p className="text-sm text-gray-500 mt-1">
        Share at least one account with the household to see totals here.
      </p>
    </div>
  )
}

function StatCard({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="text-xs uppercase tracking-wide text-gray-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-gray-900">{children}</div>
    </div>
  )
}

function SmallStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="text-xs uppercase tracking-wide text-gray-500">{label}</div>
      <div className="mt-0.5 text-sm font-medium text-gray-800">{value}</div>
    </div>
  )
}
