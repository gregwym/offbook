import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { LineChart, Pencil, Plus, Trash2, Upload } from 'lucide-react'
import { AccountVisibilityChip } from '../components/AccountVisibilityChip'
import { AmountDisplay } from '../components/AmountDisplay'
import { ImportTransactionsModal } from '../components/ImportTransactionsModal'
import { PIIPanel } from '../components/PIIPanel'
import { SyncStatusPill } from '../components/SyncStatusPill'
import { TradeFormModal } from '../components/TradeFormModal'
import { useAccountsStore } from '../store/accountsStore'
import { useScopeStore } from '../store/scopeStore'
import {
  ACCOUNT_TYPES,
  type Account,
  type AccountType,
  type CreateAccountInput,
  type UpdateAccountInput,
} from '../types/account'

export function AccountsPage() {
  const { accounts, loading, error, fetch, update, remove } = useAccountsStore()
  // householdId is null when the user belongs to no household — the
  // visibility column is omitted entirely in that case (private-only
  // mode), matching the rule in `.claude/rules/frontend.md`: don't
  // surface a column the user can't act on.
  const householdId = useScopeStore((s) => s.householdId)
  const [editing, setEditing] = useState<Account | null>(null)
  // Trade-entry surface — only available for brokerage-style accounts
  // (matches backend brokerageAccountTypes in service/trade_service.go).
  const [tradingOn, setTradingOn] = useState<Account | null>(null)
  // Per-account CSV import (#330) — opens the shared modal preset to this
  // account, the v6 §03 "per-account add more" affordance.
  const [importingInto, setImportingInto] = useState<Account | null>(null)

  useEffect(() => {
    void fetch()
  }, [fetch])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Accounts</h1>
          <p className="mt-1 text-sm text-gray-500">Manage your accounts and PII.</p>
        </div>
        {/* New accounts originate from /accounts/add per v6 §03 — keep the
            inline edit modal for changing existing accounts, but route fresh
            creation through the two-tile picker so Plaid + manual share an
            entry point. */}
        <Link
          to="/accounts/add"
          className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> Add account
        </Link>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="mt-6 overflow-x-auto rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="px-4 py-2 text-left">Institution</th>
              <th className="px-4 py-2 text-left">Type</th>
              <th className="px-4 py-2 text-left">Last 4</th>
              <th className="px-4 py-2 text-right">Balance</th>
              <th className="px-4 py-2 text-center">Active</th>
              <th className="px-4 py-2 text-left">Sync</th>
              {householdId != null && <th className="px-4 py-2 text-left">Household visibility</th>}
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {loading && accounts.length === 0 && (
              <tr><td colSpan={householdId != null ? 9 : 8} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && accounts.length === 0 && (
              <tr><td colSpan={householdId != null ? 9 : 8} className="px-4 py-6 text-center text-gray-400">No accounts yet.</td></tr>
            )}
            {accounts.map((a) => (
              <tr key={a.id} className="hover:bg-gray-50">
                <td className="px-4 py-2 font-medium text-gray-900">{a.name}</td>
                <td className="px-4 py-2 text-gray-700">{a.institution_slug}</td>
                <td className="px-4 py-2 text-gray-700">{a.account_type}</td>
                <td className="px-4 py-2 text-gray-700">{a.last_four ?? '—'}</td>
                <td className="px-4 py-2 text-right">
                  <AmountDisplay amount={a.balance} currency={a.currency} />
                </td>
                <td className="px-4 py-2 text-center">
                  <span className={a.is_active ? 'text-emerald-700' : 'text-gray-400'}>
                    {a.is_active ? 'yes' : 'no'}
                  </span>
                </td>
                <td className="px-4 py-2">
                  <SyncStatusPill account={a} />
                </td>
                {householdId != null && (
                  <td className="px-4 py-2">
                    <AccountVisibilityChip accountID={a.id} householdID={householdId} />
                  </td>
                )}
                <td className="px-4 py-2 text-right">
                  {isBrokerage(a) && (
                    <button
                      type="button"
                      onClick={() => setTradingOn(a)}
                      className="mr-2 text-gray-500 hover:text-indigo-700"
                      aria-label="Record trade"
                      title="Record trade"
                    >
                      <LineChart size={16} />
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => setImportingInto(a)}
                    className="mr-2 text-gray-500 hover:text-indigo-700"
                    aria-label="Import transactions from CSV"
                    title="Import CSV"
                  >
                    <Upload size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditing(a)}
                    className="mr-2 text-gray-500 hover:text-gray-900"
                    aria-label="Edit"
                  >
                    <Pencil size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={async () => {
                      if (window.confirm(`Delete "${a.name}"?`)) {
                        await remove(a.id)
                      }
                    }}
                    className="text-gray-500 hover:text-red-700"
                    aria-label="Delete"
                  >
                    <Trash2 size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {importingInto && (
        <ImportTransactionsModal
          accounts={accounts}
          initialAccountID={importingInto.id}
          onClose={() => setImportingInto(null)}
          onImported={() => { void fetch() }}
        />
      )}

      {editing && (
        <AccountFormModal
          mode="edit"
          account={editing}
          onClose={() => setEditing(null)}
          onSubmit={async (input) => {
            await update(editing.id, input)
            setEditing(null)
          }}
        />
      )}

      {tradingOn && (
        <TradeFormModal
          account={tradingOn}
          onClose={() => setTradingOn(null)}
          onRecorded={() => {
            // Account balance is derived from positions × prices, so the
            // accounts list refresh picks up the post-trade value.
            void fetch()
          }}
        />
      )}
    </div>
  )
}

// isBrokerage mirrors backend service.brokerageAccountTypes — only these
// account shapes accept trades.
function isBrokerage(a: Account): boolean {
  return a.account_type === 'investment' || a.account_type === 'crypto'
}

type FormProps = {
  mode: 'create' | 'edit'
  account?: Account
  onClose: () => void
  onSubmit: (input: CreateAccountInput | UpdateAccountInput) => Promise<void>
}

// Single form used for both create and edit. In edit mode, PII access is
// available inline (PIIPanel fetches on-demand).
function AccountFormModal({ mode, account, onClose, onSubmit }: FormProps) {
  const [name, setName] = useState(account?.name ?? '')
  const [institution, setInstitution] = useState(account?.institution_slug ?? '')
  const [accountType, setAccountType] = useState<AccountType>(account?.account_type ?? 'checking')
  const [currency, setCurrency] = useState(account?.currency ?? 'USD')
  const [balance, setBalance] = useState(account?.balance ?? '0')
  const [lastFour, setLastFour] = useState(account?.last_four ?? '')
  const [isActive, setIsActive] = useState(account?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const validate = (): string | null => {
    if (!name.trim()) return 'Name is required.'
    if (!institution.trim()) return 'Institution is required.'
    if (currency.trim().length !== 3) return 'Currency must be a 3-letter code.'
    if (lastFour && !/^\d{4}$/.test(lastFour)) return 'Last 4 must be exactly 4 digits.'
    return null
  }

  const submit = async () => {
    const v = validate()
    if (v) {
      setError(v)
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      await onSubmit({
        name: name.trim(),
        institution_slug: institution.trim(),
        account_type: accountType,
        currency: currency.trim().toUpperCase(),
        balance,
        last_four: lastFour.trim() === '' ? null : lastFour.trim(),
        is_active: isActive,
      })
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-lg rounded-lg bg-white shadow-xl">
        <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          {mode === 'create' ? 'Add account' : `Edit "${account?.name}"`}
        </div>
        <div className="space-y-3 px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Name">
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Institution slug">
            <input className={inputClass} value={institution} onChange={(e) => setInstitution(e.target.value)} placeholder="e.g. chase" />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Type">
              <select className={inputClass} value={accountType} onChange={(e) => setAccountType(e.target.value as AccountType)}>
                {ACCOUNT_TYPES.map((t) => (
                  <option key={t} value={t}>{t}</option>
                ))}
              </select>
            </Field>
            <Field label="Currency">
              <input className={inputClass} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Balance">
              <input className={inputClass} value={balance} onChange={(e) => setBalance(e.target.value)} inputMode="decimal" />
            </Field>
            <Field label="Last 4 (optional)">
              <input className={inputClass} value={lastFour ?? ''} maxLength={4} onChange={(e) => setLastFour(e.target.value)} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
            Active
          </label>
          {mode === 'edit' && account && <PIIPanel accountID={account.id} />}
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : mode === 'create' ? 'Create' : 'Save'}
          </button>
        </div>
      </div>
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

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
