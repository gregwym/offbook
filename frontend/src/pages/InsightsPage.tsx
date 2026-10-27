// InsightsPage — the v6 review surface. Single page, five bands, served
// from useScopedInsights so both /insights (personal) and /h/insights
// (household) render the same component. Read-only by design; clickable
// affordances are limited to category drill-down and "full page →" pills.
//
// The 5 bands map to docs/designs/App Hierarchy v6.html §06:
//   1) Net worth headline + trend line
//   2) Allocation (donut by asset kind)
//   3) Spending by category (current month; personal can step back, #607)
//   4) Budgets + Goals at-a-glance
//   5) Account list summary
import { subDecimal } from '../lib/decimal'
import { spendingHref } from '../lib/spendingLink'
import { useState } from 'react'
import { usePastMonthSpending } from '../hooks/usePastMonthSpending'
import { MonthStepper } from '../components/MonthStepper'
import { currentMonth, monthLabel, monthWindow, rangeLabel } from '../lib/month'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  LineChart as LineChartIcon,
  PieChart as PieChartIcon,
  PiggyBank,
  RefreshCw,
  Target,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import {
  Bar,
  BarChart,
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
import { assetKindLabel } from '../types/asset'
import { PartialBadge } from '../components/PartialBadge'
import { SetPriceModal } from '../components/SetPriceModal'
import { DEFAULT_TREND_MONTHS, TREND_RANGES, useScopedInsights, type InsightsData } from '../hooks/useScopedInsights'
import { useScopeStore } from '../store/scopeStore'
import { SCOPE_HOUSEHOLD } from '../types/scope'

const num = (s: string): number => Number.parseFloat(s) || 0

// PriceTarget pre-fills Set price with the asset the headline names (#599);
// a fiat target opens it as an exchange-rate entry.
type PriceTarget = { symbol: string; kind: string; quote?: string; quoteAssetId?: number }
type SetPriceHandler = (target?: PriceTarget) => void

export function InsightsPage() {
  const result = useScopedInsights()
  const { active, householdId } = useScopeStore()
  // true opens a blank Set price; an asset pre-fills it (#599).
  const [settingPrice, setSettingPrice] = useState<false | true | PriceTarget>(false)

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

  // Price tools only when there is something to price (#629): a cash-only
  // book in its own currency has none, and a fresh signup (#520) has nothing.
  const showPriceTools = result.state === 'ready' && hasPriceableAssets(result.data)

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Insights</h1>
          <p className="mt-1 text-sm text-gray-500">
            {active === SCOPE_HOUSEHOLD
              ? 'Household aggregates over shared accounts. PII never leaves each member’s book.'
              : 'Net worth · allocation · spending · budgets · goals — at a glance.'}
          </p>
        </div>
        {showPriceTools && (
          <div className="flex shrink-0 flex-wrap items-start gap-2">
            <button
              type="button"
              onClick={() => setSettingPrice(true)}
              title="Set a price yourself for an asset no provider covers — a private fund, obscure ticker, or collectible."
              className="inline-flex items-center gap-1.5 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
            >
              Set price
            </button>
            <RefreshPricesButton onRefreshed={result.reload} />
          </div>
        )}
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

      {result.state === 'ready' && (
        <InsightsBody
          data={result.data}
          onSetPrice={(target) => setSettingPrice(target ?? true)}
          trendMonths={result.trendMonths}
          trendLoading={result.trendLoading}
          trendError={result.trendError}
          onTrendMonths={result.setTrendMonths}
        />
      )}

      {settingPrice && (
        <SetPriceModal
          initial={settingPrice === true ? undefined : settingPrice}
          onClose={() => setSettingPrice(false)}
          onSet={result.reload}
        />
      )}
    </div>
  )
}

function InsightsBody({
  data,
  onSetPrice,
  ...range
}: { data: InsightsData; onSetPrice: SetPriceHandler } & TrendRangeProps) {
  // Personal fresh-signup state: no accounts at all → single primary CTA
  // per v6 §02 A2, instead of five empty bands. Household scope keeps its
  // existing "share an account" empty state on the AccountsBand.
  if (isFreshSignup(data)) {
    return <FreshSignupEmpty />
  }

  return (
    <>
      <NetWorthBand data={data} onSetPrice={onSetPrice} {...range} />
      <AllocationBand data={data} onSetPrice={onSetPrice} />
      <SpendingBand data={data} />
      <BudgetsGoalsBand data={data} />
      <AccountsBand data={data} />
      <p className="text-xs text-gray-400">
        This period: {rangeLabel(data.period.from, data.period.to)}
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

// hasPriceableAssets is true when the scope holds something a price or an
// exchange rate values: a non-cash asset class, an account in another
// currency than the headline, or a headline already flagged partial.
// Household scope has no headline currency, so more than one account
// currency stands in for "foreign" there.
function hasPriceableAssets(data: InsightsData): boolean {
  if (isFreshSignup(data)) return false
  if (!data.net_worth_complete) return true
  if (data.allocation.some((r) => r.kind !== 'fiat')) return true
  const currencies = new Set(data.accounts.map((a) => a.currency.toUpperCase()))
  const base = data.net_worth_currency?.toUpperCase()
  return base ? [...currencies].some((c) => c !== base) : currencies.size > 1
}

function isFreshSignup(data: InsightsData): boolean {
  return data.scope === 'personal' && data.accounts.length === 0
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
type TrendRangeProps = {
  trendMonths: number
  trendLoading: boolean
  trendError: boolean
  onTrendMonths: (months: number) => void
}

function NetWorthBand({
  data,
  onSetPrice,
  trendMonths,
  trendLoading,
  trendError,
  onTrendMonths,
}: { data: InsightsData; onSetPrice: SetPriceHandler } & TrendRangeProps) {
  const trend = data.net_worth_trend
  const unpriced = data.net_worth_complete ? [] : (data.net_worth_unpriced ?? [])
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
            {!data.net_worth_complete && (
              <>
                <PartialBadge title="Some assets have no recent price or exchange rate — this net worth is a partial sum. Try Refresh prices." />
                {unpriced.length === 0 && (
                  <button
                    type="button"
                    onClick={() => onSetPrice()}
                    className="ml-1.5 align-middle text-[11px] font-medium text-indigo-600 hover:text-indigo-800"
                  >
                    Set price
                  </button>
                )}
              </>
            )}
          </div>
          {unpriced.length > 0 && (
            <ul className="mt-1 space-y-0.5 text-xs text-amber-700">
              {unpriced.map((u) => {
                const fiat = u.kind === 'fiat'
                const quote = u.quote ?? data.net_worth_currency
                return (
                  <li key={`${u.asset_id}-${quote ?? ''}`}>
                    <span className="whitespace-nowrap">
                      {fiat ? 'Needs an exchange rate: ' : 'Needs a price: '}
                      <span className="font-medium">
                        {fiat && quote ? `${u.symbol} → ${quote}` : u.symbol}
                      </span>
                    </span>
                    <button
                      type="button"
                      onClick={() => onSetPrice({ symbol: u.symbol, kind: u.kind, quote, quoteAssetId: u.quote_asset_id })}
                      aria-label={fiat ? `Set exchange rate for ${u.symbol}` : `Set price for ${u.symbol}`}
                      className="ml-1.5 min-h-9 font-medium text-indigo-600 underline hover:text-indigo-800 md:min-h-0"
                    >
                      {fiat ? 'Set rate' : 'Set price'}
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </div>
        <div className="md:col-span-2">
          <div className="mb-1 flex flex-wrap items-center justify-end gap-x-2 gap-y-1">
            {trendLoading && <span className="text-[11px] text-gray-400">Loading…</span>}
            {trendError && (
              <span role="alert" className="text-[11px] text-red-600">
                Couldn’t load that range — still showing the previous one.
              </span>
            )}
            {/* A range can't change a single month-end (#629); keep it once
                the user has picked one, so they can always switch back. */}
            {(trend.length > 1 || trendMonths !== DEFAULT_TREND_MONTHS) && (
              <div role="group" aria-label="Net worth date range" className="inline-flex rounded-md border border-gray-200 p-0.5">
                {TREND_RANGES.map((r) => (
                  <button
                    key={r.months}
                    type="button"
                    aria-pressed={trendMonths === r.months}
                    onClick={() => onTrendMonths(r.months)}
                    className={`min-h-8 min-w-9 rounded px-2 text-xs font-medium ${
                      trendMonths === r.months
                        ? 'bg-indigo-600 text-white'
                        : 'text-gray-600 hover:bg-gray-100'
                    }`}
                  >
                    {r.label}
                  </button>
                ))}
              </div>
            )}
          </div>
          {trend.length === 0 ? (
            <div className="flex h-full items-center justify-center py-6 text-sm text-gray-400">
              No trend data yet — net worth will appear here as snapshots accumulate.
            </div>
          ) : trend.length === 1 ? (
            // One known month-end is not a line (#618): the server omits the
            // months before any account data instead of plotting them as $0.
            <>
              <div className="flex items-center justify-center py-6 text-sm text-gray-500">
                History starts {monthLabel(trend[0].date.slice(0, 7))} — the trend appears after another month-end.
              </div>
              <KindBreakdown trend={trend} />
            </>
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
                  {/* Linear, not monotone: a smoothed curve invents a gradual
                      rise between month-ends that never happened (#618). */}
                  <Line
                    isAnimationActive={false}
                    type="linear"
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
              <KindBreakdown trend={trend} />
            </>
          )}
        </div>
      </div>
    </section>
  )
}

// Asset-class drill-in (#374): first vs latest month-end value per kind, so
// "what drove the change" is answerable without leaving Insights. Personal
// both scopes (household by_kind comes from the aggregator, shared accounts only).
function KindBreakdown({ trend }: { trend: InsightsData['net_worth_trend'] }) {
  const first = trend[0]
  const last = trend[trend.length - 1]
  if (!last?.by_kind?.length) return null
  const startOf = new Map((first?.by_kind ?? []).map((k) => [k.kind, k.value]))
  return (
    <details className="mt-2 text-xs">
      <summary className="cursor-pointer font-medium text-indigo-600 hover:text-indigo-800">
        By asset class
      </summary>
      <table className="mt-1 w-full">
        <caption className="sr-only">Net worth by asset class, start vs latest month-end</caption>
        <thead>
          <tr className="text-left text-gray-500">
            <th scope="col" className="py-1 font-medium">Class</th>
            <th scope="col" className="py-1 text-right font-medium">{first.date.slice(0, 7)}</th>
            <th scope="col" className="py-1 text-right font-medium">{last.date.slice(0, 7)}</th>
            <th scope="col" className="py-1 text-right font-medium">Change</th>
          </tr>
        </thead>
        <tbody>
          {last.by_kind.map((k) => {
            const before = startOf.get(k.kind) ?? '0'
            const delta = subDecimal(k.value, before)
            return (
              <tr key={k.kind} className="border-t border-gray-100">
                <th scope="row" className="py-1 text-left font-normal text-gray-700">
                  {assetKindLabel(k.kind)}
                  {!k.complete && <PartialBadge title="Some holdings of this class have no price — partial." />}
                </th>
                <td className="py-1 text-right"><AmountDisplay amount={before} /></td>
                <td className="py-1 text-right"><AmountDisplay amount={k.value} /></td>
                <td className="py-1 text-right"><AmountDisplay amount={delta} /></td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </details>
  )
}

// ───── Band 2: allocation donut ─────
function AllocationBand({ data, onSetPrice }: { data: InsightsData; onSetPrice: SetPriceHandler }) {
  const rows = data.allocation
  const total = rows.reduce((acc, r) => acc + num(r.value), 0)
  const hasPartial = rows.some((r) => !r.complete)
  // One asset class is a 100% donut that says nothing (#629): collapse the
  // card to a single line so the spending band moves up.
  if (rows.length === 1) {
    const only = rows[0]
    return (
      <section className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-gray-200 bg-white px-5 py-3">
        <div className="flex items-center gap-2">
          <PieChartIcon size={16} className="text-gray-500" />
          <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
            Asset allocation
          </h2>
        </div>
        <p className="text-sm text-gray-700">
          {assetKindLabel(only.kind)} · <AmountDisplay amount={only.value} /> · 100%
          {!only.complete && (
            <>
              <PartialBadge title="Some positions of this kind have no recent price — this value understates the true total." />
              <button
                type="button"
                onClick={() => onSetPrice()}
                className="ml-1.5 text-[11px] font-medium text-indigo-600 hover:text-indigo-800"
              >
                Set price
              </button>
            </>
          )}
        </p>
      </section>
    )
  }
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
                {/* No entry animation (#524): a ~1.5s sweep left the card
                    looking empty on load and in screenshot evidence. */}
                <Pie
                  isAnimationActive={false}
                  data={rows.map((r, i) => ({
                    name: assetKindLabel(r.kind),
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
                    {assetKindLabel(r.kind)}
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
        <div className="mt-2 flex items-center gap-2 text-[11px] text-amber-700">
          <span>
            Buckets marked “partial” hold assets without prices; the donut and percentages are
            computed from the priced portion only.
          </span>
          <button
            type="button"
            onClick={() => onSetPrice()}
            className="shrink-0 font-medium text-indigo-600 hover:text-indigo-800"
          >
            Set price
          </button>
        </div>
      )}
    </section>
  )
}

// ───── Band 3: spending by category, plus depth (#367) ─────
// Depth sub-views (trend / merchants / cash flow) live inside this same
// wireframe band rather than as new top-level bands — the v6 IA locks 5
// bands; these three answer "where does my money go over time" within the
// spending band's existing scope.
function SpendingBand({ data }: { data: InsightsData }) {
  // Drill-down to the rows behind a figure (#571) — personal scope only;
  // household figures are aggregates with no member transaction list.
  const drill = data.scope === 'personal'
  // Month stepper (#607; household since #619): step the category list, the
  // month's totals, the trend and top merchants back to an earlier month for
  // the month-end review. null is the current month, served from the page's
  // own load.
  const [month, setMonth] = useState<string | null>(null)
  const past = usePastMonthSpending(month, data.scope)
  const period = month ? monthWindow(month) : { from: data.period.from, to: data.period.to }
  const rows = month ? (past.state === 'ready' ? past.byCategory : []) : data.by_category
  const merchants = month ? (past.state === 'ready' ? past.topMerchants : []) : data.top_merchants
  // The month vs. the months before it (#611) follows the stepper too.
  const trend = month ? (past.state === 'ready' ? past.categoryTrend : []) : data.category_trend
  // The month's income / spending / net (#609): the current month comes
  // from the page's cash-flow series, an earlier one from its own load.
  const totals = month
    ? past.state === 'ready' ? past.cashFlow : null
    : data.cash_flow.find((m) => m.month.slice(0, 7) === currentMonth()) ?? null
  // A month with nothing in or out leaves the "No spending" line alone.
  const hasTotals = totals != null && (num(totals.inflow) !== 0 || num(totals.outflow) !== 0)
  const busy = month != null && past.state === 'loading'
  const failed = month != null && past.state === 'error'
  const when = month ? `in ${monthLabel(month)}` : 'in this period yet'
  const max = rows.reduce((acc, r) => Math.max(acc, num(r.amount)), 0)
  return (
    <section className="rounded-lg border border-gray-200 bg-white p-5">
      <div className="flex items-center gap-2">
        <TrendingUp size={16} className="text-gray-500" />
        <h2 className="text-sm font-medium uppercase tracking-wider text-gray-500">
          Spending by category · {monthLabel(month ?? currentMonth())}
        </h2>
      </div>
      <MonthStepper month={month} onChange={setMonth} label="Spending month" className="mt-3" />
      {busy ? (
        <div className="mt-3 py-6 text-center text-sm text-gray-400" role="status">
          Loading {monthLabel(month)}…
        </div>
      ) : failed ? (
        <div className="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800" role="alert">
          <span>Couldn’t load spending for {monthLabel(month)}.</span>
          <button type="button" onClick={past.retry} className="font-medium text-amber-900 underline hover:no-underline">
            Retry
          </button>
        </div>
      ) : (
        <>
          {hasTotals && (
            <MonthTotals
              totals={totals!}
              label={monthLabel(month ?? currentMonth())}
              spendingHref={drill ? spendingHref({ start: period.from, end: period.to }) : undefined}
            />
          )}
          {rows.length === 0 ? (
            <div className="mt-3 py-6 text-center text-sm text-gray-400">
              No spending {when}.
            </div>
          ) : (
            <ul className="mt-3 space-y-1.5 text-sm">
              {rows.map((r) => {
                const v = num(r.amount)
                const isNetRefund = v < 0
                const pct = max > 0 && v > 0 ? (v / max) * 100 : 0
                return (
                  <li key={`${r.category_id ?? 'null'}-${r.name}`} className="flex items-center gap-3">
                    {drill ? (
                      <Link
                        to={spendingHref({ start: period.from, end: period.to, categoryID: r.category_id })}
                        title={`See the transactions behind ${r.name}`}
                        className="w-32 shrink-0 truncate text-indigo-700 hover:text-indigo-900 hover:underline"
                      >
                        {r.name}
                      </Link>
                    ) : (
                      <span className="w-32 shrink-0 truncate text-gray-700">{r.name}</span>
                    )}
                    <div className="relative h-2 flex-1 rounded bg-gray-100">
                      <div
                        className="absolute inset-y-0 left-0 rounded bg-indigo-400"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <span className="min-w-28 shrink-0 text-right text-gray-700">
                      <AmountDisplay amount={r.amount} />
                      {isNetRefund && (
                        <span className="block text-xs text-gray-400">net refund</span>
                      )}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}

      <div className="mt-5 grid grid-cols-1 gap-5 border-t border-gray-100 pt-4 lg:grid-cols-3">
        <CashFlowTrend months={data.cash_flow} />
        <CategoryTrendList
          items={trend}
          title={`${monthLabel(month ?? currentMonth())} vs. average`}
          empty={busy ? 'Loading…' : failed ? 'Not loaded.' : undefined}
        />
        <TopMerchantsList
          rows={merchants}
          title={month ? `Top merchants · ${monthLabel(month)}` : 'Top merchants'}
          empty={busy ? 'Loading…' : failed ? 'Not loaded.' : month ? `No merchant spend in ${monthLabel(month)}.` : 'No merchant spend yet.'}
          hrefFor={drill ? (merchant) => spendingHref({ start: period.from, end: period.to, merchant }) : undefined}
        />
      </div>

      {data.scope === 'personal' && <RecurringList items={data.recurring} />}
    </section>
  )
}

// MonthTotals is the month-end answer in figures (#609): what came in, what
// went out, what was kept. Spending uses the category rows' predicate, so it
// equals their sum, and it drills into exactly those rows.
function MonthTotals({
  totals,
  label,
  spendingHref,
}: {
  totals: InsightsData['cash_flow'][number]
  label: string
  // Personal only: household spending is an aggregate with no row list.
  spendingHref?: string
}) {
  const overspent = totals.net.trim().startsWith('-')
  // Narrow screens: one label · amount line per figure; sm+: three tiles.
  const cell =
    'flex items-baseline justify-between gap-3 rounded-md bg-gray-50 px-3 py-2 sm:flex-col sm:items-start sm:justify-start sm:gap-0'
  const term = 'text-xs font-medium uppercase tracking-wider text-gray-500'
  return (
    <dl aria-label={`${label} totals`} className="mt-3 grid grid-cols-1 gap-1.5 text-sm sm:grid-cols-3 sm:gap-2">
      <div className={cell}>
        <dt className={term}>Income</dt>
        <dd className="text-base font-medium text-gray-900 sm:mt-0.5">
          <AmountDisplay amount={totals.inflow} />
        </dd>
      </div>
      <div className={cell}>
        <dt className={term}>Spending</dt>
        <dd className="text-base font-medium sm:mt-0.5">
          {spendingHref ? (
            <Link
              to={spendingHref}
              title={`See the transactions behind ${label} spending`}
              className="text-indigo-700 hover:text-indigo-900 hover:underline"
            >
              <AmountDisplay amount={totals.outflow} />
            </Link>
          ) : (
            <span className="text-gray-900">
              <AmountDisplay amount={totals.outflow} />
            </span>
          )}
        </dd>
      </div>
      <div className={cell}>
        <dt className={term}>Net</dt>
        <dd className="text-right text-base font-medium sm:mt-0.5 sm:text-left">
          <AmountDisplay amount={totals.net} signed />
          <span className="block text-xs font-normal text-gray-500">
            {overspent ? 'Spent more than came in' : 'Kept after spending'}
          </span>
        </dd>
      </div>
    </dl>
  )
}

// Deterministic recurring-charge detection (#368) — personal scope only.
// Merchant, cadence, last amount, next-expected date, monthly-equivalent
// cost; a read-only lens over real transactions, nothing invented.
function RecurringList({ items }: { items: InsightsData['recurring'] }) {
  return (
    <div className="mt-5 border-t border-gray-100 pt-4">
      <h3 className="text-xs font-medium uppercase tracking-wider text-gray-400">
        Recurring charges
      </h3>
      {items.length === 0 ? (
        <div className="mt-2 py-4 text-center text-xs text-gray-400">
          No recurring charges detected yet.
        </div>
      ) : (
        <table className="mt-2 w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-gray-400">
              <th className="pb-1 font-medium">Merchant</th>
              <th className="pb-1 font-medium">Cadence</th>
              <th className="pb-1 font-medium">Last charge</th>
              <th className="pb-1 font-medium">Next expected</th>
              <th className="pb-1 text-right font-medium">Monthly cost</th>
            </tr>
          </thead>
          <tbody>
            {items.map((r) => (
              <tr key={r.merchant} className="border-t border-gray-50">
                <td className="py-1.5 text-gray-700">{r.merchant}</td>
                <td className="py-1.5 capitalize text-gray-500">{r.cadence}</td>
                <td className="py-1.5 text-gray-500">
                  <AmountDisplay amount={r.last_amount} /> on {r.last_date}
                </td>
                <td className="py-1.5 text-gray-500">{r.next_expected_date}</td>
                <td className="py-1.5 text-right text-gray-700">
                  <AmountDisplay amount={r.monthly_equivalent} />/mo
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// Income vs. spending trend by month (#367). Reuses the CashFlow endpoint
// already backing the legacy DashboardCharts component; independently
// fetched, so a missing series just renders an empty state.
function CashFlowTrend({ months }: { months: InsightsData['cash_flow'] }) {
  return (
    <div>
      <h3 className="text-xs font-medium uppercase tracking-wider text-gray-400">
        Income vs. spending
      </h3>
      {months.length === 0 ? (
        <div className="mt-2 py-4 text-center text-xs text-gray-400">Not enough data yet.</div>
      ) : (
        <ResponsiveContainer width="100%" height={140}>
          <BarChart
            data={months.map((m) => ({
              month: m.month.slice(0, 7),
              inflow: num(m.inflow),
              outflow: -num(m.outflow),
            }))}
            stackOffset="sign"
          >
            <XAxis dataKey="month" tick={{ fontSize: 10 }} />
            <YAxis hide />
            <Tooltip formatter={(v) => (typeof v === 'number' ? Math.abs(v).toFixed(2) : String(v))} />
            <Bar isAnimationActive={false} dataKey="inflow" stackId="0" fill="#10B981" name="Inflow" />
            <Bar isAnimationActive={false} dataKey="outflow" stackId="0" fill="#EF4444" name="Outflow" />
          </BarChart>
        </ResponsiveContainer>
      )}
    </div>
  )
}

// Month-over-month category trend: this-month vs. trailing average, top 5
// by this-month spend (#367).
// title names the month when the band is stepped back (#607): this list
// always compares the current month with its trailing average.
function CategoryTrendList({
  items,
  title = 'Trending vs. average',
  empty = 'Not enough history yet.',
}: {
  items: InsightsData['category_trend']
  title?: string
  empty?: string
}) {
  const top = items.slice(0, 5)
  return (
    <div>
      <h3 className="text-xs font-medium uppercase tracking-wider text-gray-400">
        {title}
      </h3>
      {top.length === 0 ? (
        <div className="mt-2 py-4 text-center text-xs text-gray-400">{empty}</div>
      ) : (
        <ul className="mt-2 space-y-1.5 text-sm">
          {top.map((c) => {
            const thisMonth = num(c.this_month)
            const avg = num(c.trailing_average)
            const delta = avg > 0 ? ((thisMonth - avg) / avg) * 100 : null
            return (
              <li
                key={`${c.category_id ?? 'null'}-${c.name}`}
                className="flex items-center justify-between gap-2"
              >
                <span className="truncate text-gray-700">{c.name}</span>
                <span className="shrink-0 text-right text-gray-500">
                  <AmountDisplay amount={c.this_month} />
                  {delta != null && (
                    <span className={delta > 0 ? 'ml-1 text-red-600' : 'ml-1 text-emerald-600'}>
                      {delta > 0 ? '▲' : '▼'} {Math.abs(delta).toFixed(0)}%
                    </span>
                  )}
                </span>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}

// Top spending merchants, period-scoped (#367).
function TopMerchantsList({
  rows,
  hrefFor,
  title = 'Top merchants',
  empty = 'No merchant spend yet.',
}: {
  rows: InsightsData['top_merchants']
  hrefFor?: (merchant: string) => string
  title?: string
  empty?: string
}) {
  const top = rows.slice(0, 5)
  return (
    <div>
      <h3 className="text-xs font-medium uppercase tracking-wider text-gray-400">
        {title}
      </h3>
      {top.length === 0 ? (
        <div className="mt-2 py-4 text-center text-xs text-gray-400">
          {empty}
        </div>
      ) : (
        <ul className="mt-2 space-y-1.5 text-sm">
          {top.map((m) => (
            <li key={m.merchant} className="flex items-center justify-between gap-2">
              <span className="truncate text-gray-700">
                {hrefFor ? (
                  <Link
                    to={hrefFor(m.merchant)}
                    title={`See the transactions behind ${m.merchant}`}
                    className="text-indigo-700 hover:text-indigo-900 hover:underline"
                  >
                    {m.merchant}
                  </Link>
                ) : (
                  m.merchant
                )}{' '}
                <span className="text-xs text-gray-400">×{m.count}</span>
              </span>
              <span className="shrink-0 text-right text-gray-500">
                <AmountDisplay amount={m.amount} />
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
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
              // Same figures as the Budgets card (#613): spent of available
              // (limit + rollover), the measure pct and the bar use.
              const carried = Number(b.carryover) > 0
              return (
                <li key={b.id}>
                  <div className="flex items-start justify-between gap-3">
                    <span className="min-w-0">
                      {b.period_start && b.period_end ? (
                        <Link
                          to={spendingHref({ start: b.period_start, end: b.period_end, categoryID: b.category_id })}
                          title={`See the transactions behind the ${b.category_name} budget`}
                          className="text-indigo-700 hover:text-indigo-900 hover:underline"
                        >
                          {b.category_name}
                        </Link>
                      ) : (
                        <span className="text-gray-700">{b.category_name}</span>
                      )}
                      {b.period !== 'monthly' && (
                        <span className="ml-1.5 text-xs text-gray-500">{b.period}</span>
                      )}
                    </span>
                    <span className={['shrink-0 text-right', over ? 'text-red-700' : 'text-gray-700'].join(' ')}>
                      <AmountDisplay amount={b.spent} /> / <AmountDisplay amount={b.available} />
                    </span>
                  </div>
                  {carried && (
                    <div className="mt-0.5 text-right text-xs text-emerald-700">
                      includes <AmountDisplay amount={b.carryover} /> rolled over
                    </div>
                  )}
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
              <span className="truncate text-gray-900">
                {a.name}
                {a.needs_attention && (
                  <Link
                    to={`/accounts/${a.id}/reconciliation`}
                    title="An adjustment on this account needs review — see the reconciliation view."
                    className="ml-1.5 inline-flex items-center gap-1 rounded border border-amber-200 bg-amber-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-amber-700 hover:bg-amber-100"
                  >
                    <AlertTriangle size={10} /> needs attention
                  </Link>
                )}
              </span>
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
