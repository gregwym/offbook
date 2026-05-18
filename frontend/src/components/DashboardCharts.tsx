// Recharts is loaded eagerly here; the dashboard is the only consumer for
// now. Each component fetches its own data — keeps the surface
// independent so a transient failure on one chart doesn't blank the rest.
import { useEffect, useState } from 'react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { getCashFlow, getNetWorth, getSpendByCategory } from '../api/dashboard'
import type {
  CashFlowMonth,
  NetWorthPoint,
  SpendByCategoryItem,
} from '../types/dashboard'

// Money strings come in as NUMERIC text. parseFloat is fine for chart
// scaling; AmountDisplay handles user-facing precision elsewhere.
const num = (s: string): number => Number.parseFloat(s) || 0

const FALLBACK_PIE_COLORS = ['#6366F1', '#F59E0B', '#10B981', '#EC4899', '#3B82F6', '#EF4444', '#A855F7', '#64748B']

export function SpendByCategoryChart() {
  const [data, setData] = useState<SpendByCategoryItem[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void getSpendByCategory()
      .then((rows) => { if (!cancelled) setData(rows) })
      .catch((e) => { if (!cancelled) setError(errMsg(e)) })
    return () => { cancelled = true }
  }, [])

  return (
    <ChartCard title="Spending by category (this month)">
      {error && <div className="text-sm text-red-700">{error}</div>}
      {!error && !data && <div className="text-sm text-gray-400">Loading…</div>}
      {!error && data && data.length === 0 && (
        <div className="text-sm text-gray-400">No outflows this month.</div>
      )}
      {!error && data && data.length > 0 && (
        <ResponsiveContainer width="100%" height={260}>
          <PieChart>
            <Pie
              data={data.map((d) => ({ name: d.name, value: num(d.amount), color: d.color }))}
              dataKey="value"
              nameKey="name"
              cx="50%"
              cy="50%"
              outerRadius={90}
              label={(p: { name?: string }) => p.name ?? ''}
            >
              {data.map((d, i) => (
                <Cell key={i} fill={d.color && d.color.length > 0 ? d.color : FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length]} />
              ))}
            </Pie>
            <Tooltip formatter={(v) => (typeof v === 'number' ? v.toFixed(2) : String(v))} />
          </PieChart>
        </ResponsiveContainer>
      )}
    </ChartCard>
  )
}

export function CashFlowChart() {
  const [data, setData] = useState<CashFlowMonth[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void getCashFlow(12)
      .then((rows) => { if (!cancelled) setData(rows) })
      .catch((e) => { if (!cancelled) setError(errMsg(e)) })
    return () => { cancelled = true }
  }, [])

  return (
    <ChartCard title="Cash flow (last 12 months)">
      {error && <div className="text-sm text-red-700">{error}</div>}
      {!error && !data && <div className="text-sm text-gray-400">Loading…</div>}
      {!error && data && (
        <ResponsiveContainer width="100%" height={260}>
          <BarChart
            data={data.map((d) => ({
              month: d.month.slice(0, 7), // YYYY-MM
              inflow: num(d.inflow),
              outflow: -num(d.outflow), // negative so it sits below the axis
            }))}
            stackOffset="sign"
          >
            <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
            <XAxis dataKey="month" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} />
            <Tooltip formatter={(v) => (typeof v === 'number' ? v.toFixed(2) : String(v))} />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Bar dataKey="inflow" stackId="0" fill="#10B981" name="Inflow" />
            <Bar dataKey="outflow" stackId="0" fill="#EF4444" name="Outflow" />
          </BarChart>
        </ResponsiveContainer>
      )}
    </ChartCard>
  )
}

export function NetWorthChart() {
  const [data, setData] = useState<NetWorthPoint[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void getNetWorth(12)
      .then((rows) => { if (!cancelled) setData(rows) })
      .catch((e) => { if (!cancelled) setError(errMsg(e)) })
    return () => { cancelled = true }
  }, [])

  return (
    <ChartCard
      title="Net worth (last 12 months)"
      footnote="Approximated by back-deriving from current balances. Manual balance edits won't be reflected."
    >
      {error && <div className="text-sm text-red-700">{error}</div>}
      {!error && !data && <div className="text-sm text-gray-400">Loading…</div>}
      {!error && data && (
        <ResponsiveContainer width="100%" height={260}>
          <LineChart data={data.map((d) => ({ date: d.date.slice(0, 7), total: num(d.total) }))}>
            <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
            <XAxis dataKey="date" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} />
            <Tooltip formatter={(v) => (typeof v === 'number' ? v.toFixed(2) : String(v))} />
            <Line type="monotone" dataKey="total" stroke="#6366F1" strokeWidth={2} dot={false} />
          </LineChart>
        </ResponsiveContainer>
      )}
    </ChartCard>
  )
}

function ChartCard({ title, footnote, children }: { title: string; footnote?: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-gray-500">{title}</h3>
      {children}
      {footnote && <p className="mt-2 text-xs text-gray-400">{footnote}</p>}
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
