import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { LineChart, Plus, TrendingUp } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { useAccountsStore } from '../store/accountsStore'
import { useInvestmentsStore } from '../store/investmentsStore'
import type { Account } from '../types/account'
import type { CreateInvestmentInput, Investment } from '../types/investment'

// Quantity formatter: NUMERIC(30,18) — show up to 18 fractional digits,
// trim trailing zeros so common stock quantities don't render as
// "10.000000000000000000". Floats here would silently corrupt crypto
// holdings (BTC has 8 places, ETH has 18), so we work on the raw string.
function formatQuantity(amount: string | null | undefined): string {
  if (!amount) return '—'
  const trimmed = amount.trim()
  if (!trimmed.includes('.')) return trimmed
  // Strip trailing zeros and a now-dangling decimal point.
  return trimmed.replace(/(\.\d*?)0+$/, '$1').replace(/\.$/, '')
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
            Holdings are append-only snapshots — each save records a point in time, never overwrites history.
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
      <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-20 animate-pulse rounded-lg border border-gray-200 bg-gray-50" />
        ))}
      </div>
    )
  }
  if (!portfolio) return null
  const gl = portfolio.total_unrealized_gain_loss
  const glNegative = gl !== null && gl.trim().startsWith('-')
  return (
    <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Tile label="Total value">
        <AmountDisplay amount={portfolio.total_market_value} currency="USD" />
      </Tile>
      <Tile label="Cost basis">
        <AmountDisplay amount={portfolio.total_cost_basis} currency="USD" />
      </Tile>
      <Tile label="Unrealized G/L">
        {gl === null ? (
          <span className="text-gray-400">—</span>
        ) : (
          <AmountDisplay
            amount={gl}
            currency="USD"
            className={glNegative ? 'text-red-700' : 'text-emerald-700'}
          />
        )}
      </Tile>
      <Tile label="Holdings">
        <span className="text-gray-900">{portfolio.holdings_count}</span>
      </Tile>
    </div>
  )
}

function Tile({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white px-4 py-3 shadow-sm">
      <p className="text-xs font-medium uppercase tracking-wide text-gray-500">{label}</p>
      <p className="mt-1 text-lg font-semibold">{children}</p>
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
            <Th>Ticker</Th>
            <Th>Name</Th>
            <Th>Account</Th>
            <Th>Asset class</Th>
            <Th align="right">Quantity</Th>
            <Th align="right">Cost basis</Th>
            <Th align="right">Market value</Th>
            <Th align="right">Unrealized G/L</Th>
            <Th>Snapshot</Th>
            <Th>Source</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 bg-white">
          {holdings.map((h) => {
            const gl = computeGainLoss(h)
            return (
              <tr
                key={h.id}
                onClick={() => onRowClick(h)}
                className="cursor-pointer hover:bg-gray-50"
              >
                <Td className="font-medium text-gray-900">{h.ticker}</Td>
                <Td>{h.name ?? '—'}</Td>
                <Td>{accountsById.get(h.account_id)?.name ?? `#${h.account_id}`}</Td>
                <Td>{h.asset_class ?? <span className="text-gray-400">Unclassified</span>}</Td>
                <Td align="right" className="font-mono">{formatQuantity(h.quantity)}</Td>
                <Td align="right"><AmountDisplay amount={h.cost_basis} currency="USD" /></Td>
                <Td align="right"><AmountDisplay amount={h.market_value} currency="USD" /></Td>
                <Td align="right">
                  {gl === null ? (
                    <span className="text-gray-400">—</span>
                  ) : (
                    <AmountDisplay
                      amount={gl}
                      currency="USD"
                      className={gl.startsWith('-') ? 'text-red-700' : 'text-emerald-700'}
                    />
                  )}
                </Td>
                <Td>{h.snapshot_date}</Td>
                <Td className="capitalize text-gray-500">{h.source}</Td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

// computeGainLoss subtracts two decimal strings without losing precision.
// Returns null when either field is missing. We keep the math here in
// strings via BigInt scaled to 18 fractional places — matches the
// NUMERIC(30,18) the backend persists.
function computeGainLoss(h: Investment): string | null {
  if (!h.market_value || !h.cost_basis) return null
  return subDecimal(h.market_value, h.cost_basis)
}

function subDecimal(a: string, b: string): string {
  const scale = 18
  const toScaled = (s: string): bigint => {
    const [int = '0', frac = ''] = s.trim().split('.')
    const padded = (frac + '0'.repeat(scale)).slice(0, scale)
    const sign = int.startsWith('-') ? -1n : 1n
    const absInt = int.replace('-', '')
    return sign * (BigInt(absInt) * 10n ** BigInt(scale) + BigInt(padded))
  }
  const diff = toScaled(a) - toScaled(b)
  const sign = diff < 0n ? '-' : ''
  const abs = diff < 0n ? -diff : diff
  const base = 10n ** BigInt(scale)
  const intPart = abs / base
  const fracPart = (abs % base).toString().padStart(scale, '0').replace(/0+$/, '')
  return fracPart ? `${sign}${intPart}.${fracPart}` : `${sign}${intPart}`
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
    <td className={`px-3 py-2 ${align === 'right' ? 'text-right' : 'text-left'} ${className}`}>
      {children}
    </td>
  )
}

type AddProps = {
  accounts: Account[]
  onClose: () => void
  onSubmit: (input: CreateInvestmentInput) => Promise<void>
}

function AddHoldingModal({ accounts, onClose, onSubmit }: AddProps) {
  // Default to investment-typed accounts when present; otherwise any
  // account. Empty list is handled in the render path.
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
                  <Th>Source</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 bg-white">
                {rows.map((r) => (
                  <tr key={r.id}>
                    <Td>{r.snapshot_date}</Td>
                    <Td align="right" className="font-mono">{formatQuantity(r.quantity)}</Td>
                    <Td align="right"><AmountDisplay amount={r.cost_basis} currency="USD" /></Td>
                    <Td align="right"><AmountDisplay amount={r.market_value} currency="USD" /></Td>
                    <Td className="capitalize text-gray-500">{r.source}</Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      <ModalFooter submitting={false} submitLabel="Close" onCancel={onClose} onSubmit={async () => onClose()} />
    </Modal>
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
