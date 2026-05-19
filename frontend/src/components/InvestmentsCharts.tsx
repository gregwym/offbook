// Investment-specific charts. Lives next to DashboardCharts.tsx so chart
// idioms stay close together. Data arrives pre-aggregated from the
// backend portfolio summary — the component just renders.
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip } from 'recharts'
import type { AssetClassAllocation } from '../types/investment'
import { FALLBACK_PIE_COLORS } from './chartColors'

const num = (s: string): number => Number.parseFloat(s) || 0

type Props = {
  data: AssetClassAllocation[] | null | undefined
  // totalMarketValue lets us hide the chart for an empty portfolio even
  // when the by_asset_class slice has an "Unclassified" bucket with 0 mv
  // (closed positions, etc.).
  totalMarketValue: string | undefined
}

export function AllocationDonut({ data, totalMarketValue }: Props) {
  const empty = !data || data.length === 0 || (totalMarketValue !== undefined && num(totalMarketValue) === 0)

  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-gray-500">Allocation by asset class</h3>
      {empty ? (
        <div className="py-8 text-center text-sm text-gray-400">Add a holding to see your allocation.</div>
      ) : (
        <ResponsiveContainer width="100%" height={260}>
          <PieChart>
            <Pie
              data={(data ?? []).map((a, i) => ({
                name: a.asset_class,
                value: num(a.market_value),
                weight: num(a.weight_pct),
                color: FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length],
              }))}
              dataKey="value"
              nameKey="name"
              cx="50%"
              cy="50%"
              innerRadius={50}
              outerRadius={90}
              label={(p: { name?: string; weight?: number }) =>
                p.name ? `${p.name} ${(p.weight ?? 0).toFixed(1)}%` : ''
              }
            >
              {(data ?? []).map((_, i) => (
                <Cell key={i} fill={FALLBACK_PIE_COLORS[i % FALLBACK_PIE_COLORS.length]} />
              ))}
            </Pie>
            <Tooltip
              formatter={(value, name, item: { payload?: { weight?: number } }) => {
                const weight = item?.payload?.weight ?? 0
                const v = typeof value === 'number' ? value.toFixed(2) : String(value)
                return [`${v} (${weight.toFixed(1)}%)`, name]
              }}
            />
          </PieChart>
        </ResponsiveContainer>
      )}
    </div>
  )
}
