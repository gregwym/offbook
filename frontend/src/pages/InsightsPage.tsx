// InsightsPage — the v6 review surface. Single page, five bands, served
// from useScopedInsights so both /insights (personal) and /h/insights
// (household) render the same component. Read-only by design; clickable
// affordances are limited to category drill-down and "full page →" pills.
//
// The 5 bands map to docs/designs/App Hierarchy v6.html §06:
//   1) Net worth headline + trend line
//   2) Allocation (donut by asset kind)
//   3) Spending by category (current period)
//   4) Budgets + Goals at-a-glance
//   5) Account list summary
import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  LineChart as LineChartIcon,
  PieChart as PieChartIcon,
  PiggyBank,
  RefreshCw,
  Target,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import {
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { refreshPrices } from '../api/prices'
import { AmountDisplay } from '../components/AmountDisplay'
import { FALLBACK_PIE_COLORS } from '../components/chartColors'
import { PartialBadge } from '../components/PartialBadge'
import { useScopedInsights, type InsightsData } from '../hooks/useScopedInsights'
import { useScopeStore } from '../store/scopeStore'
import { SCOPE_HOUSEHOLD } from '../types/scope'

const num = (s: string): number => Number.parseFloat(s) || 0

export function InsightsPage() {
  const result = useScopedInsights()
  const { active, householdId } = useScopeStore()

  if (active === SCOPE_HOUSEHOLD && householdId == null) {
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
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Insights</h1>
          <p className="mt-1 text-sm text-gray-500">
            {active === SCOPE_HOUSEHOLD
              ? 'Household aggregates over shared accounts. PII never leaves each member’s book.'
              : 'Net worth · allocation · spending · budgets · goals — at a glance.'}
          </p>
        </div>
        <RefreshPricesButton onRefreshed={result.reload} />
      </div>

      {result.state === 'loading' && (
        <div className="rounded-lg border border-gray-200 bg-white px-5 py-8 text-center text-sm text-gray-400">
          Loading…
        </div>
      )}

      {result.state === 'error' && (
        <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {result.error}
        </div>
      )}

      {result.state === 'ready' && <InsightsBody data={result.data} />}
    </div>
  )
}

function InsightsBody({ data }: { data: InsightsData }) {
  // Personal fresh-signup state: no accounts at all → single primary CTA
  // per v6 §02 A2, instead of five empty bands. Household scope keeps its
  // existing "share an account" empty state on the AccountsBand.
  if (data.scope === 'personal' && data.accounts.length === 0) {
    return <FreshSignupEmpty />
  }

  return (
    <>
      <NetWorthBand data={data} />
      <AllocationBand data={data} />
      <SpendingBand data={data} />
      <BudgetsGoalsBand data={data} />
      <AccountsBand data={data} />
      <p className="text-xs text-gray-400">
        {data.period.from.slice(0, 10)} → {data.period.to.slice(0, 10)}
      </p>
    </>
  )
}

// RefreshPricesButton triggers the user-initiated price refresh (ADR-0014
// Phase 1). Clicking is the egress consent: only the user's held symbols
// go upstream. On success the page data reloads so valuations pick up the
// fresh observations; skipped symbols are surfaced, not hidden.
function RefreshPricesButton({ onRefreshed }: { onRefreshed: () => void }) {
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const run = async () => {
    setBusy(true)
    setNote(null)
    setError(null)
    try {
      const r = await refreshPrices()
      const parts = [`${r.refreshed} price${r.refreshed === 1 ? '' : 's'} refreshed`]
      if (r.skipped.length > 0) parts.push(`no source for ${r.skipped.join(', ')}`)
      setNote(parts.join(' · '))
      onRefreshed()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'refresh failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex shrink-0 flex-col items-end gap-1">
      <button
        type="button"
        onClick={() => void run()}
        disabled={busy}
        title="Fetch current market prices for the assets you hold. Sends only the symbol list to the price provider."
        className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50"
      >
        <RefreshCw size={14} className={busy ? 'animate-spin' : undefined} />
        {busy ? 'Refreshing…' : 'Refresh prices'}
      </button>
      {note && <span className="text-[11px] text-gray-500">{note}</span>}
      {error && <span className="text-[11px] text-red-600">{error}</span>}
    </div>
  )
}

function FreshSignupEmpty() {
  return (
    <div className="rounded-lg border border-dashed border-gray-300 bg-white p-10 text-center">
      <Wallet size={32} className="mx-auto text-gray-300 mb-3" />
      <h2 className="text-lg font-medium text-gray-900">Add your first account</h2>
      <p className="mt-1 text-sm text-gray-500">
        Connect a bank or add one manually. Your net worth, allocation, and
        spending will populate as soon as transactions arrive.
      </p>
      <Link
        to="/accounts/add"
        className="mt-6 inline-flex items-center gap-2 rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
      >
        Add your first account →
      </Link>
    </div>
  )
}

// ───── Band 1: net worth headline + trend ─────
function NetWorthBand({ data }: { data: InsightsData }) {
  const trend = data.net_worth_trend
  const hasPartialMonths = trend.some((p) => !p.complete)
  return (
    <section className="rounded-lg border border-gray-200 bg-white p-5">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div>
          <div className="text-xs font-medium uppercase tracking-wider text-gray-500">
            Net worth
          </div>
          <div className="mt-1 text-3xl font-semibold text-gray-900">
            <AmountDisplay amount={data.net_worth} />
          </div>
          <div className="mt-1 text-xs text-gray-400">
            Income (period): <AmountDisplay amount={data.income} /> · Spending:{' '}
            <AmountDisplay amount={data.spending} />
          </div>
        </div>
        <div className="md:col-span-2">
          {trend.length === 0 ? (
            <div className="flex h-full items-center justify-center py-6 text-sm text-gray-400">
              No trend data yet — net worth will appear here as snapshots accumulate.
            </div>
          ) : (
            <>
              <ResponsiveContainer width="100%" height={hasPartialMonths ? 124 : 140}>
                <LineChart
                  data={trend.map((p) => ({
                    date: p.date.slice(0, 7),
                    total: num(p.value),
                    complete: p.complete,
                  }))}
                >
                  <XAxis dataKey="date" tick={{ fontSize: 10 }} />
                  <YAxis hide />
                  <Tooltip
                    formatter={(v, _name, item) => {
                      const text = typeof v === 'number' ? v.toFixed(2) : String(v)
                      const point = item?.payload as { complete?: boolean } | undefined
                      return point?.complete === false ? `${text} (partial)` : text
                    }}
                  />
                  {/* Months with an unpriced asset render a hollow amber dot —
                      the value plotted there is a partial sum (#339). */}
                  <Line
                    type="monotone"
                    dataKey="total"
                    stroke="#6366F1"
                    strokeWidth={2}
                    dot={(props: { cx?: number; cy?: number; payload?: { complete?: boolean } }) =>
                      props.payload?.complete === false && props.cx != null && props.cy != null ? (
                        <circle
                          key={`partial-${props.cx}-${props.cy}`}
                          cx={props.cx}
                          cy={props.cy}
                          r={3.5}
                          fill="#FFFBEB"
                          stroke="#D97706"
                          strokeWidth={1.5}
                        />
                      ) : (
                        // recharts requires an element; render nothing visible.
                        <circle key={`dot-${props.cx}-${props.cy}`} r={0} />
                      )
                    }
                  />
                </LineChart>
              </ResponsiveContainer>
              {hasPartialMonths && (
                <div className="mt-1 text-[11px] text-amber-700">
                  ◌ marked months include assets without prices — those totals are partial.
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </section>
  )
}

// ───── Band 2: allocation donut ─────
function AllocationBand({ data }: { data: InsightsData }) {
  const rows = data.allocation
  const total = rows.reduce((acc, r) => acc + num(r.value), 0)
  const hasPartial = rows.some((r) => !r.complete)
  return (
    <section className="rounded-lg border border-gray-200 bg-white p-5">
      <div className="flex items-center gap-2">
        <PieChartIcon size={16} className="text-gray-500" />
        <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
          Asset allocation
        </h2>
      </div>
      {rows.length === 0 ? (
        <div className="mt-3 py-6 text-center text-sm text-gray-400">
          No investments yet — add a brokerage or crypto account to see allocation.
        </div>
      ) : (
        <div className="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2">
          {/* total === 0 with rows present means every position is unpriced —
              skip the empty donut but keep the list, so holdings aren't
              misreported as "no investments" (#339). */}
          {total > 0 && (
            <ResponsiveContainer width="100%" height={220}>
              <PieChart>
                <Pie
                  data={rows.map((r, i) => ({
                    name: r.kind,
                    value: num(r.value),
                    color: FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length],
                  }))}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  innerRadius={50}
                  outerRadius={85}
                  label={(p: { name?: string }) => p.name ?? ''}
                >
                  {rows.map((_, i) => (
                    <Cell key={i} fill={FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length]} />
                  ))}
                </Pie>
                <Tooltip formatter={(v) => (typeof v === 'number' ? v.toFixed(2) : String(v))} />
              </PieChart>
            </ResponsiveContainer>
          )}
          <ul className="flex flex-col justify-center gap-1.5 text-sm">
            {rows.map((r, i) => {
              const pct = total > 0 ? (num(r.value) / total) * 100 : 0
              return (
                <li key={r.kind} className="flex items-center justify-between">
                  <span className="flex items-center gap-2 text-gray-700">
                    <span
                      className="inline-block h-2.5 w-2.5 rounded-full"
                      style={{ background: FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length] }}
                    />
                    {r.kind}
                  </span>
                  <span className="text-gray-500">
                    <AmountDisplay amount={r.value} /> · {pct.toFixed(1)}%
                    {!r.complete && (
                      <PartialBadge title="Some positions of this kind have no recent price — this value (and the percentages) understate the true allocation." />
                    )}
                  </span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
      {hasPartial && (
        <div className="mt-2 text-[11px] text-amber-700">
          Buckets marked “partial” hold assets without prices; the donut and percentages are
          computed from the priced portion only.
        </div>
      )}
    </section>
  )
}

// ───── Band 3: spending by category ─────
function SpendingBand({ data }: { data: InsightsData }) {
  const rows = data.by_category
  const max = rows.reduce((acc, r) => Math.max(acc, num(r.amount)), 0)
  return (
    <section className="rounded-lg border border-gray-200 bg-white p-5">
      <div className="flex items-center gap-2">
        <TrendingUp size={16} className="text-gray-500" />
        <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
          Spending by category · this period
        </h2>
      </div>
      {rows.length === 0 ? (
        <div className="mt-3 py-6 text-center text-sm text-gray-400">
          No spending in this period yet.
        </div>
      ) : (
        <ul className="mt-3 space-y-1.5 text-sm">
          {rows.map((r) => {
            const v = num(r.amount)
            const pct = max > 0 ? (v / max) * 100 : 0
            return (
              <li key={`${r.category_id ?? 'null'}-${r.name}`} className="flex items-center gap-3">
                <span className="w-32 shrink-0 truncate text-gray-700">{r.name}</span>
                <div className="relative h-2 flex-1 rounded bg-gray-100">
                  <div
                    className="absolute inset-y-0 left-0 rounded bg-indigo-400"
                    style={{ width: `${pct}%` }}
                  />
                </div>
                <span className="w-24 shrink-0 text-right text-gray-700">
                  <AmountDisplay amount={r.amount} />
                </span>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

// ───── Band 4: budgets + goals at a glance ─────
function BudgetsGoalsBand({ data }: { data: InsightsData }) {
  const budgetsHref = data.scope === 'household' ? '/h/budgets' : '/budgets'
  const goalsHref = data.scope === 'household' ? '/h/goals' : '/savings-goals'
  return (
    <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <div className="rounded-lg border border-gray-200 bg-white p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Target size={16} className="text-gray-500" />
            <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
              Budgets · this period
            </h2>
          </div>
          <Link to={budgetsHref} className="text-xs text-indigo-600 hover:text-indigo-800">
            full page →
          </Link>
        </div>
        {data.budgets.length === 0 ? (
          <div className="mt-3 py-6 text-center text-sm text-gray-400">
            No active budgets — create one to start tracking spend.
          </div>
        ) : (
          <ul className="mt-3 space-y-2 text-sm">
            {data.budgets.map((b) => {
              const pct = Math.min(Math.max(b.pct, 0), 1.2) * 100
              const over = b.pct > 1
              const warn = b.pct > 0.8 && !over
              const barColor = over ? 'bg-red-500' : warn ? 'bg-amber-500' : 'bg-indigo-400'
              return (
                <li key={b.id}>
                  <div className="flex items-center justify-between">
                    <span className="text-gray-700">{b.category_name}</span>
                    <span className={over ? 'text-red-700' : 'text-gray-700'}>
                      <AmountDisplay amount={b.spent} /> / <AmountDisplay amount={b.limit} />
                    </span>
                  </div>
                  <div className="mt-1 h-2 rounded bg-gray-100">
                    <div className={`${barColor} h-full rounded`} style={{ width: `${pct}%` }} />
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      <div className="rounded-lg border border-gray-200 bg-white p-5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <PiggyBank size={16} className="text-gray-500" />
            <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
              Savings goals
            </h2>
          </div>
          <Link to={goalsHref} className="text-xs text-indigo-600 hover:text-indigo-800">
            full page →
          </Link>
        </div>
        {data.goals.length === 0 ? (
          <div className="mt-3 py-6 text-center text-sm text-gray-400">
            No goals yet — set a target to start tracking progress.
          </div>
        ) : (
          <ul className="mt-3 space-y-2 text-sm">
            {data.goals.map((g) => {
              const pct = Math.min(Math.max(g.progress_pct, 0), 1) * 100
              return (
                <li key={g.id}>
                  <div className="flex items-center justify-between">
                    <span className="text-gray-700">{g.name}</span>
                    <span className="text-gray-500">
                      <AmountDisplay amount={g.current} /> / <AmountDisplay amount={g.target} />
                    </span>
                  </div>
                  <div className="mt-1 h-2 rounded bg-gray-100">
                    <div className="h-full rounded bg-emerald-500" style={{ width: `${pct}%` }} />
                  </div>
                  {g.target_date && (
                    <div className="mt-0.5 text-[11px] text-gray-400">target {g.target_date}</div>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </section>
  )
}

// ───── Band 5: account list summary ─────
function AccountsBand({ data }: { data: InsightsData }) {
  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <header className="flex items-center gap-2 border-b border-gray-200 px-5 py-3">
        <LineChartIcon size={16} className="text-gray-500" />
        <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
          Accounts feeding this
        </h2>
      </header>
      {data.accounts.length === 0 ? (
        <div className="px-5 py-6 text-center text-sm text-gray-400">
          {data.scope === 'household'
            ? 'No shared accounts — visit Accounts (personal) and set visibility to feed this household.'
            : 'No accounts yet — add your first account to start.'}
        </div>
      ) : (
        <div className="divide-y divide-gray-100">
          {data.accounts.map((a) => (
            <div
              key={a.id}
              className="grid grid-cols-[1fr_auto_auto_auto] items-center gap-3 px-5 py-2 text-sm"
            >
              <span className="truncate text-gray-900">{a.name}</span>
              <span className="text-xs text-gray-400">{a.account_type}</span>
              <span className="text-xs text-gray-400 capitalize">{a.source}</span>
              <span className="text-right">
                <AmountDisplay amount={a.balance} currency={a.currency || 'USD'} />
                {!a.balance_complete && <PartialBadge />}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
