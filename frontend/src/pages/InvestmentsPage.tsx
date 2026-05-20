// Layout follows the M6 Investments wireframe (docs/designs/App Hierarchy
// v4.html, the "INVESTMENTS" sketch): three tiles up top (total · cost
// basis · unrealized G/L), then the allocation donut, then a 4-column
// holdings table — Holding · Shares·units · Price · Value. Crypto rows
// shrink their quantity font so the full NUMERIC(30,18) precision fits
// inline, which is the literal M6 acceptance criterion for #113. Polished
// hi-fi treatment is deferred to the paired M9+ frontend milestone per
// the roadmap.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { LineChart, Plus, TrendingUp } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { DateDisplay } from '../components/DateDisplay'
import { AllocationDonut } from '../components/InvestmentsCharts'
import { useAccountsStore } from '../store/accountsStore'
import { useInvestmentsStore } from '../store/investmentsStore'
import type { Account } from '../types/account'
import type { CreateInvestmentInput, Investment } from '../types/investment'

const QUANTITY_SCALE = 18

// formatQuantity preserves all NUMERIC(30,18) digits while trimming
// trailing zeros — Number() would lose precision past 15 digits and
// silently corrupt crypto holdings, so string work is mandatory.
function formatQuantity(amount: string | null | undefined): string {
  if (!amount) return '—'
  const trimmed = amount.trim()
  if (!trimmed.includes('.')) return trimmed
  return trimmed.replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '')
}

// isHighPrecision flags rows that need the smaller crypto-style font:
// either the holding is tagged 'crypto'/'cryptocurrency', or its
// quantity carries more than 8 fractional digits (BTC's 8, anything
// beyond is ETH-style 18-digit territory).
function isHighPrecision(inv: Investment): boolean {
  const cls = inv.asset_class?.toLowerCase() ?? ''
  if (cls === 'crypto' || cls === 'cryptocurrency') return true
  const dot = inv.quantity.indexOf('.')
  if (dot < 0) return false
  const frac = inv.quantity.slice(dot + 1).replace(/0+$/, '')
  return frac.length > 8
}

// pricePerUnit = market_value / quantity, returned as a decimal string.
// Done with BigInt scaled to 18 fractional digits to keep crypto-scale
// values honest. Returns null when either operand is missing or quantity
// is zero. The result is for display only (AmountDisplay clips to the
// currency's default precision).
function pricePerUnit(inv: Investment): string | null {
  if (!inv.market_value || !inv.quantity) return null
  const qScaled = toScaled(inv.quantity)
  if (qScaled === 0n) return null
  const mvScaled = toScaled(inv.market_value)
  // (mv * 10^scale) / q gives a result that is itself scaled by 10^scale.
  const ratio = (mvScaled * 10n ** BigInt(QUANTITY_SCALE)) / qScaled
  return fromScaled(ratio)
}

// computeGainLoss = market_value - cost_basis, null when either is missing.
function computeGainLoss(h: Investment): string | null {
  if (!h.market_value || !h.cost_basis) return null
  return fromScaled(toScaled(h.market_value) - toScaled(h.cost_basis))
}

function toScaled(s: string): bigint {
  const [int = '0', frac = ''] = s.trim().split('.')
  const padded = (frac + '0'.repeat(QUANTITY_SCALE)).slice(0, QUANTITY_SCALE)
  const sign = int.startsWith('-') ? -1n : 1n
  const absInt = int.replace('-', '')
  return sign * (BigInt(absInt) * 10n ** BigInt(QUANTITY_SCALE) + BigInt(padded))
}

function fromScaled(v: bigint): string {
  const sign = v < 0n ? '-' : ''
  const abs = v < 0n ? -v : v
  const base = 10n ** BigInt(QUANTITY_SCALE)
  const intPart = abs / base
  const fracPart = (abs % base).toString().padStart(QUANTITY_SCALE, '0').replace(/0+$/, '')
  return fracPart ? `${sign}${intPart}.${fracPart}` : `${sign}${intPart}`
}

export function InvestmentsPage() {
  const { holdings, portfolio, loading, error, fetch, create, fetchHistory, clearError } =
    useInvestmentsStore()
  const { accounts, fetch: fetchAccounts } = useAccountsStore()
  const [adding, setAdding] = useState(false)
  const [historyFor, setHistoryFor] = useState<Investment | null>(null)

  useEffect(() => {
    void fetch()
    void fetchAccounts()
  }, [fetch, fetchAccounts])

  const accountsById = useMemo(() => {
    const m = new Map<number, Account>()
    for (const a of accounts) m.set(a.id, a)
    return m
  }, [accounts])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Investments</h1>
          <p className="mt-1 text-sm text-gray-500">
            Stocks · ETFs · crypto in one ledger. Each save is an append-only snapshot — history is never overwritten.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> Add holding
        </button>
      </div>

      {error && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <PortfolioTiles portfolio={portfolio} loading={loading && !portfolio} />

      <div className="mt-6">
        <AllocationDonut
          data={portfolio?.by_asset_class}
          totalMarketValue={portfolio?.total_market_value}
        />
      </div>

      <div className="mt-6 rounded-lg border border-gray-200 bg-white shadow-sm">
        {loading && holdings.length === 0 && (
          <div className="px-4 py-10 text-center text-sm text-gray-400">Loading…</div>
        )}
        {!loading && holdings.length === 0 && (
          <div className="px-4 py-10 text-center text-sm text-gray-500">
            <TrendingUp size={24} className="mx-auto mb-1 text-gray-300" />
            No holdings yet — add one to start tracking your portfolio.
          </div>
        )}
        {holdings.length > 0 && (
          <HoldingsTable
            holdings={holdings}
            accountsById={accountsById}
            onRowClick={(inv) => setHistoryFor(inv)}
          />
        )}
      </div>

      {adding && (
        <AddHoldingModal
          accounts={accounts}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input)
            setAdding(false)
          }}
        />
      )}
      {historyFor && (
        <SnapshotHistoryModal
          inv={historyFor}
          account={accountsById.get(historyFor.account_id)}
          fetchHistory={fetchHistory}
          onClose={() => setHistoryFor(null)}
        />
      )}
    </div>
  )
}

function PortfolioTiles({
  portfolio,
  loading,
}: {
  portfolio: ReturnType<typeof useInvestmentsStore.getState>['portfolio']
  loading: boolean
}) {
  if (loading) {
    return (
      <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-20 animate-pulse rounded-lg border border-gray-200 bg-gray-50" />
        ))}
      </div>
    )
  }
  if (!portfolio) return null
  const rc = portfolio.recent_change ?? null
  const rcNegative = rc !== null && rc.delta.trim().startsWith('-')
  return (
    <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
      <Tile label="Total value">
        <AmountDisplay amount={portfolio.total_market_value} currency="USD" />
      </Tile>
      <Tile label="Cost basis">
        <AmountDisplay amount={portfolio.total_cost_basis} currency="USD" />
      </Tile>
      {/* Recent change replaces the wireframe's "today's P&L" tile. We
          measure "today" as "between the two most recent snapshot dates"
          since there is no live price feed yet — see #122. */}
      <Tile
        label={rc ? recentLabel(rc) : "Recent change"}
        subtitle={rc ? `${rc.up} of ${rc.holdings_compared} up` : 'one snapshot only'}
      >
        {rc === null ? (
          <span className="text-gray-400">—</span>
        ) : (
          <AmountDisplay
            amount={rc.delta}
            currency="USD"
            className={rcNegative ? 'text-red-700' : 'text-emerald-700'}
          />
        )}
      </Tile>
    </div>
  )
}

// recentLabel formats the latest/prior date pair as a compact tile label.
// When the dates are the same day (rare — multiple snapshots stamped the
// same date), fall back to a single date.
function recentLabel(rc: import('../types/investment').RecentChange): string {
  const latest = rc.latest_date.slice(0, 10)
  const prior = rc.prior_date.slice(0, 10)
  if (latest === prior) return `Change · ${latest}`
  return `Change · ${prior} → ${latest}`
}

function Tile({
  label,
  children,
  subtitle,
}: {
  label: string
  children: ReactNode
  subtitle?: string
}) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white px-4 py-3 shadow-sm">
      <p className="text-xs font-medium uppercase tracking-wide text-gray-500">{label}</p>
      <p className="mt-1 text-lg font-semibold">{children}</p>
      {subtitle && <p className="mt-0.5 text-[11px] text-gray-500">{subtitle}</p>}
    </div>
  )
}

function HoldingsTable({
  holdings,
  accountsById,
  onRowClick,
}: {
  holdings: Investment[]
  accountsById: Map<number, Account>
  onRowClick: (inv: Investment) => void
}) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200 text-sm">
        <thead className="bg-gray-50">
          <tr>
            <Th>Holding</Th>
            <Th align="right">Shares · units</Th>
            <Th align="right">Price</Th>
            <Th align="right">Value</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 bg-white">
          {holdings.map((h) => {
            const highPrecision = isHighPrecision(h)
            const price = pricePerUnit(h)
            const account = accountsById.get(h.account_id)
            return (
              <tr
                key={h.id}
                onClick={() => onRowClick(h)}
                className="cursor-pointer hover:bg-gray-50"
              >
                <Td>
                  <div className="font-medium text-gray-900">{h.ticker}</div>
                  <div className="text-xs text-gray-500">
                    {h.name ?? <span className="italic">unnamed</span>}
                    {h.asset_class ? ` · ${h.asset_class}` : ''}
                    {account ? ` · ${account.name}` : ''}
                  </div>
                </Td>
                <Td
                  align="right"
                  className={[
                    'tabular-nums',
                    highPrecision ? 'text-[11px] leading-tight' : '',
                  ].join(' ').trim()}
                >
                  {formatQuantity(h.quantity)}
                </Td>
                <Td align="right" className="tabular-nums">
                  <AmountDisplay amount={price} currency="USD" fractionDigits={highPrecision ? 2 : 2} />
                </Td>
                <Td align="right" className="tabular-nums">
                  <AmountDisplay amount={h.market_value} currency="USD" />
                </Td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

type AddProps = {
  accounts: Account[]
  onClose: () => void
  onSubmit: (input: CreateInvestmentInput) => Promise<void>
}

function AddHoldingModal({ accounts, onClose, onSubmit }: AddProps) {
  const investmentAccounts = useMemo(
    () => accounts.filter((a) => a.account_type === 'investment'),
    [accounts],
  )
  const usableAccounts = investmentAccounts.length > 0 ? investmentAccounts : accounts

  const [accountID, setAccountID] = useState<string>(
    usableAccounts[0]?.id ? String(usableAccounts[0].id) : '',
  )
  const [ticker, setTicker] = useState('')
  const [name, setName] = useState('')
  const [assetClass, setAssetClass] = useState('')
  const [quantity, setQuantity] = useState('0')
  const [costBasis, setCostBasis] = useState('')
  const [marketValue, setMarketValue] = useState('')
  const [snapshotDate, setSnapshotDate] = useState<string>(todayISO())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    if (!accountID) {
      setError('Pick an account.')
      return
    }
    if (!ticker.trim()) {
      setError('Ticker is required.')
      return
    }
    if (!quantity.trim() || quantity === '0' || quantity === '0.0') {
      setError('Quantity must not be zero.')
      return
    }
    setSubmitting(true)
    try {
      const input: CreateInvestmentInput = {
        account_id: Number(accountID),
        ticker: ticker.trim(),
        quantity: quantity.trim(),
        snapshot_date: snapshotDate || null,
        source: 'manual',
      }
      if (name.trim()) input.name = name.trim()
      if (assetClass.trim()) input.asset_class = assetClass.trim()
      if (costBasis.trim()) input.cost_basis = costBasis.trim()
      if (marketValue.trim()) input.market_value = marketValue.trim()
      await onSubmit(input)
    } catch (err) {
      setError(extractErr(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title="Add holding" onClose={onClose}>
      <div className="space-y-3">
        {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        {usableAccounts.length === 0 && (
          <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
            No accounts yet. Add an account on the Accounts page first.
          </div>
        )}
        <Field label="Account">
          <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
            <option value="">Select…</option>
            {usableAccounts.map((a) => (
              <option key={a.id} value={a.id}>{a.name}</option>
            ))}
          </select>
        </Field>
        <Field label="Ticker">
          <input
            className={inputClass}
            value={ticker}
            onChange={(e) => setTicker(e.target.value.toUpperCase())}
            placeholder="VTI"
          />
        </Field>
        <Field label="Name (optional)">
          <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder="Vanguard Total Stock Market" />
        </Field>
        <Field label="Asset class (optional)">
          <input className={inputClass} value={assetClass} onChange={(e) => setAssetClass(e.target.value)} placeholder="stock / bond / crypto / cash" />
        </Field>
        <Field label="Quantity (up to 18 decimal places)">
          <input className={inputClass} value={quantity} onChange={(e) => setQuantity(e.target.value)} inputMode="decimal" />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Cost basis (optional)">
            <input className={inputClass} value={costBasis} onChange={(e) => setCostBasis(e.target.value)} inputMode="decimal" />
          </Field>
          <Field label="Market value (optional)">
            <input className={inputClass} value={marketValue} onChange={(e) => setMarketValue(e.target.value)} inputMode="decimal" />
          </Field>
        </div>
        <Field label="Snapshot date">
          <input type="date" className={inputClass} value={snapshotDate} onChange={(e) => setSnapshotDate(e.target.value)} />
        </Field>
      </div>
      <ModalFooter submitting={submitting} submitLabel="Save snapshot" onCancel={onClose} onSubmit={submit} />
    </Modal>
  )
}

type HistoryProps = {
  inv: Investment
  account: Account | undefined
  fetchHistory: (accountID: number, ticker: string) => Promise<Investment[]>
  onClose: () => void
}

function SnapshotHistoryModal({ inv, account, fetchHistory, onClose }: HistoryProps) {
  const [rows, setRows] = useState<Investment[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchHistory(inv.account_id, inv.ticker)
      .then((r) => {
        if (!cancelled) setRows(r)
      })
      .catch((err) => {
        if (!cancelled) setError(extractErr(err))
      })
    return () => {
      cancelled = true
    }
  }, [fetchHistory, inv.account_id, inv.ticker])

  return (
    <Modal title={`${inv.ticker} history${account ? ` · ${account.name}` : ''}`} onClose={onClose}>
      <div>
        {error && <div className="mb-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        {!rows && !error && <div className="py-6 text-center text-sm text-gray-400">Loading…</div>}
        {rows && rows.length === 0 && (
          <div className="py-6 text-center text-sm text-gray-500">
            <LineChart size={20} className="mx-auto mb-1 text-gray-300" />
            No snapshots.
          </div>
        )}
        {rows && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 text-sm">
              <thead className="bg-gray-50">
                <tr>
                  <Th>Date</Th>
                  <Th align="right">Quantity</Th>
                  <Th align="right">Cost basis</Th>
                  <Th align="right">Market value</Th>
                  <Th align="right">Unrealized G/L</Th>
                  <Th>Source</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 bg-white">
                {rows.map((r) => {
                  const gl = computeGainLoss(r)
                  return (
                    <tr key={r.id}>
                      <Td><DateDisplay value={r.snapshot_date} /></Td>
                      <Td align="right" className="tabular-nums">{formatQuantity(r.quantity)}</Td>
                      <Td align="right" className="tabular-nums"><AmountDisplay amount={r.cost_basis} currency="USD" /></Td>
                      <Td align="right" className="tabular-nums"><AmountDisplay amount={r.market_value} currency="USD" /></Td>
                      <Td align="right" className="tabular-nums">
                        {gl === null ? <span className="text-gray-400">—</span> : (
                          <AmountDisplay
                            amount={gl}
                            currency="USD"
                            className={gl.startsWith('-') ? 'text-red-700' : 'text-emerald-700'}
                          />
                        )}
                      </Td>
                      <Td className="capitalize text-gray-500">{r.source}</Td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <ModalFooter submitting={false} submitLabel="Close" onCancel={onClose} onSubmit={async () => onClose()} />
    </Modal>
  )
}

function Th({ children, align = 'left' }: { children: ReactNode; align?: 'left' | 'right' }) {
  return (
    <th
      scope="col"
      className={`px-3 py-2 text-xs font-semibold uppercase tracking-wide text-gray-500 ${align === 'right' ? 'text-right' : 'text-left'}`}
    >
      {children}
    </th>
  )
}

function Td({
  children,
  align = 'left',
  className = '',
}: {
  children: ReactNode
  align?: 'left' | 'right'
  className?: string
}) {
  return (
    <td className={`px-3 py-2 align-top ${align === 'right' ? 'text-right' : 'text-left'} ${className}`}>
      {children}
    </td>
  )
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-2xl rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          <span>{title}</span>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-700">×</button>
        </div>
        <div className="px-5 py-4">{children}</div>
      </div>
    </div>
  )
}

function ModalFooter({
  submitting,
  submitLabel,
  onCancel,
  onSubmit,
}: {
  submitting: boolean
  submitLabel: string
  onCancel: () => void
  onSubmit: () => Promise<void>
}) {
  return (
    <div className="mt-3 flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
      <button type="button" onClick={onCancel} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
      <button
        type="button"
        onClick={onSubmit}
        disabled={submitting}
        className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
      >
        {submitting ? 'Saving…' : submitLabel}
      </button>
    </div>
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

function todayISO(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const dd = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${dd}`
}

function extractErr(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
