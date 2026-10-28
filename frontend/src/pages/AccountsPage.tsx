import { Fragment, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { AlertTriangle, CheckCircle2, LineChart, Pencil, Plus, Trash2, Upload, X } from 'lucide-react'
import { accountTransactionsHref, canUpdateBalance, isBrokerage } from '../lib/accountActions'
import { listItems } from '../api/plaid'
import { AccountVisibilityChip } from '../components/AccountVisibilityChip'
import { AmountDisplay } from '../components/AmountDisplay'
import { ImportTransactionsModal } from '../components/ImportTransactionsModal'
import { PartialBadge } from '../components/PartialBadge'
import { PIIPanel } from '../components/PIIPanel'
import { SetPriceModal } from '../components/SetPriceModal'
import { SyncStatusPill } from '../components/SyncStatusPill'
import { TimeAgo } from '../components/TimeAgo'
import { TradeFormModal } from '../components/TradeFormModal'
import { UpdateBalanceModal } from '../components/UpdateBalanceModal'
import { useMinWidth } from '../hooks/useMinWidth'
import { groupAccounts, institutionLabel, summarizeAccounts, type CurrencyTotal } from '../lib/accountGroups'
import { useAccountsStore } from '../store/accountsStore'
import { useScopeStore } from '../store/scopeStore'
import {
  ACCOUNT_TYPES,
  accountTypeLabel,
  type Account,
  type AccountType,
  type CreateAccountInput,
  type UpdateAccountInput,
} from '../types/account'
import { ModalShell } from '../components/ModalShell'

export function AccountsPage() {
  const { accounts, loading, error, fetch, update, remove } = useAccountsStore()
  // householdId is null when the user belongs to no household — the
  // visibility column is omitted entirely in that case (private-only
  // mode), matching the rule in `.claude/rules/frontend.md`: don't
  // surface a column the user can't act on.
  const householdId = useScopeStore((s) => s.householdId)
  // The visibility chip fetches and holds state, so it mounts in exactly one
  // place: its own column from md (768px) up, stacked under the name below.
  const wide = useMinWidth(768)
  const [editing, setEditing] = useState<Account | null>(null)
  // Trade-entry surface — only available for brokerage-style accounts
  // (matches backend brokerageAccountTypes in service/trade_service.go).
  const [tradingOn, setTradingOn] = useState<Account | null>(null)
  // Per-account CSV import (#330) — opens the shared modal preset to this
  // account, the v6 §03 "per-account add more" affordance.
  const [importingInto, setImportingInto] = useState<Account | null>(null)
  // Tier-1 manual price entry (#373) — asset-agnostic, so a single modal
  // instance serves every row's "partial" badge.
  // settingPrice: null = closed; 'blank' = empty form; a symbol/kind pre-fills
  // the holding the balance is missing a price for (#530).
  const [settingPrice, setSettingPrice] = useState<{ symbol: string; kind: string } | 'blank' | null>(null)
  // Manual "what does the bank say?" balance (#504) — non-Plaid cash accounts.
  const [balancing, setBalancing] = useState<Account | null>(null)
  // A manual account just created on /accounts/add (#520): confirm it and
  // offer importing its transactions as the obvious next step. Read once from
  // router state, then cleared so a reload doesn't re-show it.
  const location = useLocation()
  const navigate = useNavigate()
  const [justCreated, setJustCreated] = useState<CreatedAccount | null>(
    () => (location.state as { created?: CreatedAccount } | null)?.created ?? null,
  )
  useEffect(() => {
    if ((location.state as { created?: CreatedAccount } | null)?.created) {
      navigate(location.pathname, { replace: true, state: null })
    }
  }, [location.state, location.pathname, navigate])
  const createdAccount = justCreated ? accounts.find((a) => a.id === justCreated.id) ?? null : null

  // Delete lives in the edit modal (#657), never inline beside Edit.
  const confirmRemove = async (a: Account): Promise<boolean> => {
    if (!window.confirm(`Delete "${a.name}"?`)) return false
    await remove(a.id)
    return true
  }

  useEffect(() => {
    void fetch()
  }, [fetch])

  // Linked accounts name their institution through the Plaid item (#655);
  // only fetched when some account is linked.
  const hasLinked = accounts.some((a) => a.plaid_item_id)
  const [plaidNames, setPlaidNames] = useState<Map<string, string>>(() => new Map())
  useEffect(() => {
    if (!hasLinked) return
    listItems()
      .then(({ items }) => {
        const m = new Map<string, string>()
        for (const it of items) if (it.institution_name) m.set(it.plaid_item_id, it.institution_name)
        setPlaidNames(m)
      })
      .catch(() => {
        // The label falls back to "Linked bank"; the page stays usable.
      })
  }, [hasLinked])

  const groups = useMemo(() => groupAccounts(accounts), [accounts])
  const summary = useMemo(() => summarizeAccounts(accounts), [accounts])
  // Columns nobody fills are dropped (#657): Last 4 when no account has one,
  // Sync when no account is linked (manual accounts render no pill).
  const showLastFour = accounts.some((a) => a.last_four)
  const showSync = accounts.some((a) => a.last_sync_status !== null)
  const columns = 5 + (showLastFour ? 1 : 0) + (showSync ? 1 : 0) + (householdId != null ? 1 : 0)

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Accounts</h1>
          <p className="mt-1 text-sm text-gray-500">Balances, sync status and imports for each of your accounts.</p>
        </div>
        {/* New accounts originate from /accounts/add per v6 §03 — keep the
            inline edit modal for changing existing accounts, but route fresh
            creation through the two-tile picker so Plaid + manual share an
            entry point. */}
        <Link
          to="/accounts/add"
          className="inline-flex shrink-0 items-center whitespace-nowrap gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> Add account
        </Link>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {justCreated && (
        <div role="status" className="mt-4 flex items-start gap-3 rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm">
          <CheckCircle2 size={18} className="mt-0.5 shrink-0 text-emerald-600" aria-hidden="true" />
          <div className="min-w-0 flex-1">
            <div className="font-medium text-emerald-900">{justCreated.name} added.</div>
            <div className="mt-0.5 text-emerald-800">
              Next, bring in its transactions: import a statement from your bank (CSV, PDF or photo).
            </div>
            <button
              type="button"
              onClick={() => createdAccount && setImportingInto(createdAccount)}
              disabled={!createdAccount}
              className="mt-2 inline-flex min-h-9 items-center gap-2 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
            >
              <Upload size={14} /> Import transactions
            </button>
          </div>
          <button
            type="button"
            onClick={() => setJustCreated(null)}
            aria-label="Dismiss"
            className="-m-1 p-1 text-emerald-700 hover:text-emerald-900"
          >
            <X size={16} />
          </button>
        </div>
      )}

      {summary && (
        <dl
          aria-label="Accounts summary"
          className="mt-6 divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white sm:grid sm:grid-cols-3 sm:gap-3 sm:divide-y-0 sm:border-0 sm:bg-transparent"
        >
          <SummaryTile label="Assets" amount={summary.assets} currency={summary.currency} complete={summary.complete} />
          {/* What is owed, as a positive amount (#659); a net credit on cards/loans says so. */}
          <SummaryTile
            label="Liabilities"
            amount={summary.owed}
            currency={summary.currency}
            complete
            note={summary.liabilityCredit ? 'credit' : undefined}
          />
          <SummaryTile label="Net worth" amount={summary.netWorth} currency={summary.currency} complete={summary.complete} strong />
        </dl>
      )}
      {!summary && accounts.length > 0 && (
        <p className="mt-6 text-sm text-gray-600">
          Your accounts use more than one currency.{' '}
          <Link to="/insights" className="font-medium text-indigo-600 hover:text-indigo-800">
            See net worth in one currency on Insights
          </Link>
        </p>
      )}

      <div className="mt-4 overflow-x-auto rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="px-4 py-2 text-left">Name</th>
              <th className="hidden px-4 py-2 text-left md:table-cell">Institution</th>
              <th className="hidden px-4 py-2 text-left md:table-cell">Type</th>
              {showLastFour && <th className="hidden px-4 py-2 text-left md:table-cell">Last 4</th>}
              <th className="px-4 py-2 text-right">Balance</th>
              {showSync && <th className="hidden px-4 py-2 text-left md:table-cell">Sync</th>}
              {householdId != null && <th className="hidden px-4 py-2 text-left md:table-cell">Household visibility</th>}
              <th className="hidden px-4 py-2 md:table-cell"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          {accounts.length === 0 && (
          <tbody>
            {loading && (
              <tr><td colSpan={columns} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && (
              <tr><td colSpan={columns} className="px-4 py-8 text-center">
                <div className="text-gray-700">No accounts yet.</div>
                <div className="mt-1 text-sm text-gray-500">
                  Connect a bank or add one manually — transactions and balances appear here once an account exists.
                </div>
                <Link
                  to="/accounts/add"
                  className="mt-3 inline-flex min-h-9 items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
                >
                  <Plus size={16} /> Add your first account
                </Link>
              </td></tr>
            )}
          </tbody>
          )}
          {groups.map((g) => (
          <Fragment key={g.key}>
          <tbody aria-label={g.label} className="divide-y divide-gray-100 border-t border-gray-200">
            <tr className="bg-gray-50/70">
              <th scope="rowgroup" className="px-4 py-2 text-left text-sm font-semibold text-gray-700">
                {g.label}
                <span className="ml-1.5 text-xs font-normal text-gray-400">{g.accounts.length}</span>
              </th>
              <td colSpan={showLastFour ? 3 : 2} className="hidden md:table-cell" />
              <td className="px-4 py-2 text-right text-sm font-semibold text-gray-900" aria-label={`${g.label} total`}>
                <Subtotals totals={g.subtotals} />
              </td>
              <td colSpan={columns - (showLastFour ? 5 : 4)} className="hidden md:table-cell" />
            </tr>
            {g.accounts.map((a) => (
              <tr key={a.id} className="hover:bg-gray-50">
                <td className="px-4 py-2 align-top font-medium text-gray-900">
                  {/* The name opens this account's transactions (#657). */}
                  <Link
                    to={accountTransactionsHref(a.id)}
                    className="-my-2 inline-flex min-h-10 items-center text-gray-900 underline decoration-gray-300 underline-offset-2 hover:text-indigo-700 hover:decoration-indigo-400 md:min-h-0"
                  >
                    {a.name}
                  </Link>
                  {!a.is_active && (
                    <span className="ml-1.5 inline-flex items-center rounded border border-gray-200 bg-gray-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-gray-500">
                      Inactive
                    </span>
                  )}
                  {a.needs_attention && (
                    <Link
                      to={`/accounts/${a.id}/reconciliation`}
                      title="An adjustment on this account needs review — see the reconciliation view."
                      className="ml-1.5 inline-flex items-center gap-1 rounded border border-amber-200 bg-amber-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-amber-700 hover:bg-amber-100"
                    >
                      <AlertTriangle size={10} /> needs attention
                    </Link>
                  )}
                  {/* Narrow screens drop every column but Name and Balance
                      (#511); stack the details, sync state and labelled
                      actions here instead of behind a sideways scroll. */}
                  <div className="mt-1 space-y-1.5 font-normal md:hidden">
                    <div className="text-xs text-gray-500">
                      {[accountTypeLabel(a.account_type), institutionLabel(a, plaidNames), a.last_four ? `••${a.last_four}` : null]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                    <SyncStatusPill account={a} />
                    {householdId != null && !wide && (
                      <AccountVisibilityChip accountID={a.id} householdID={householdId} />
                    )}
                    <div className="flex flex-wrap gap-x-3 text-xs">
                      {isBrokerage(a) && (
                        <button type="button" onClick={() => setTradingOn(a)} aria-label={`Record trade in ${a.name}`} className="min-h-10 text-indigo-700 underline">
                          Trade
                        </button>
                      )}
                      <button type="button" onClick={() => setImportingInto(a)} aria-label={`Import transactions into ${a.name}`} className="min-h-10 text-indigo-700 underline">
                        Import
                      </button>
                      <button type="button" onClick={() => setEditing(a)} aria-label={`Edit ${a.name}`} className="min-h-10 text-indigo-700 underline">
                        Edit
                      </button>
                    </div>
                  </div>
                </td>
                <td className="hidden px-4 py-2 align-top text-gray-700 md:table-cell">{institutionLabel(a, plaidNames)}</td>
                <td className="hidden px-4 py-2 align-top text-gray-700 md:table-cell">{accountTypeLabel(a.account_type)}</td>
                {showLastFour && <td className="hidden px-4 py-2 align-top text-gray-700 md:table-cell">{a.last_four ?? '—'}</td>}
                <td className="px-4 py-2 text-right align-top">
                  <AmountDisplay amount={a.balance} currency={a.currency} />
                  {!a.balance_complete && (
                    <>
                      <PartialBadge />
                      {(a.unpriced_assets ?? []).length === 0 && (
                        <button
                          type="button"
                          onClick={() => setSettingPrice('blank')}
                          className="ml-1.5 align-middle text-[11px] font-medium text-indigo-600 hover:text-indigo-800"
                        >
                          Set price
                        </button>
                      )}
                      {(a.unpriced_assets ?? []).map((u) => (
                        <div key={u.asset_id} className="mt-0.5 text-xs text-amber-700">
                          <span className="whitespace-nowrap">
                            Needs a price: <span className="font-medium">{u.symbol}</span>
                          </span>
                          <button
                            type="button"
                            onClick={() => setSettingPrice({ symbol: u.symbol, kind: u.kind })}
                            aria-label={`Set price for ${u.symbol}`}
                            className="ml-1.5 min-h-9 font-medium text-indigo-600 underline hover:text-indigo-800 md:min-h-0"
                          >
                            Set price
                          </button>
                        </div>
                      ))}
                    </>
                  )}
                  {a.last_observed_at && (
                    <div className="mt-0.5 text-xs text-gray-400">
                      Observed <TimeAgo when={a.last_observed_at} /> via {a.last_observed_source}
                    </div>
                  )}
                  {canUpdateBalance(a) && (
                    <button
                      type="button"
                      onClick={() => setBalancing(a)}
                      aria-label={`Update balance for ${a.name}`}
                      className="mt-0.5 block w-full text-right text-[11px] font-medium text-indigo-600 hover:text-indigo-800"
                    >
                      Update balance
                    </button>
                  )}
                </td>
                {showSync && (
                  <td className="hidden px-4 py-2 align-top md:table-cell">
                    <SyncStatusPill account={a} />
                  </td>
                )}
                {householdId != null && (
                  <td className="hidden px-4 py-2 align-top md:table-cell">
                    {wide && <AccountVisibilityChip accountID={a.id} householdID={householdId} />}
                  </td>
                )}
                <td className="hidden whitespace-nowrap px-2 py-0.5 text-right align-top md:table-cell">
                  {isBrokerage(a) && (
                    <button
                      type="button"
                      onClick={() => setTradingOn(a)}
                      className={iconButtonClass}
                      aria-label={`Record trade in ${a.name}`}
                      title="Record trade"
                    >
                      <LineChart size={16} />
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => setImportingInto(a)}
                    className={iconButtonClass}
                    aria-label={`Import transactions into ${a.name}`}
                    title="Import transactions (CSV, PDF, or photo)"
                  >
                    <Upload size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditing(a)}
                    className={iconButtonClass}
                    aria-label={`Edit ${a.name}`}
                    title="Edit or delete"
                  >
                    <Pencil size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
          </Fragment>
          ))}
        </table>
      </div>

      {importingInto && (
        <ImportTransactionsModal
          accounts={accounts}
          initialAccountID={importingInto.id}
          onClose={() => setImportingInto(null)}
          onImported={() => {
            setJustCreated(null)
            void fetch()
          }}
          onUndone={() => { void fetch() }}
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
          onDelete={async () => {
            if (await confirmRemove(editing)) setEditing(null)
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

      {balancing && (
        <UpdateBalanceModal
          account={balancing}
          onClose={() => setBalancing(null)}
          onUpdated={() => { void fetch() }}
        />
      )}

      {settingPrice && (
        <SetPriceModal
          initial={settingPrice === 'blank' ? undefined : settingPrice}
          onClose={() => setSettingPrice(null)}
          onSet={() => {
            void fetch()
          }}
        />
      )}
    </div>
  )
}

type CreatedAccount = { id: number; name: string }

function SummaryTile({ label, amount, currency, complete, strong, note }: {
  label: string
  amount: string
  currency: string
  complete: boolean
  strong?: boolean
  note?: string
}) {
  return (
    // Phones: one card, a row per figure (label left, amount right) so a long
    // balance never squeezes. From sm up: three tiles.
    <div className="flex min-w-0 items-baseline justify-between gap-3 px-4 py-2 sm:block sm:rounded-lg sm:border sm:border-gray-200 sm:bg-white sm:py-3">
      <dt className="text-xs font-medium uppercase tracking-wider text-gray-500">{label}</dt>
      <dd className={`${strong ? 'font-semibold text-gray-900' : 'text-gray-800'} text-sm sm:mt-0.5 sm:text-lg`}>
        <AmountDisplay amount={amount} currency={currency} />
        {note && <span className="ml-1 text-xs font-normal text-gray-500">{note}</span>}
        {!complete && <PartialBadge />}
      </dd>
    </div>
  )
}

// Subtotals renders a group's total, one line per currency when it mixes them.
function Subtotals({ totals }: { totals: CurrencyTotal[] }) {
  return (
    <>
      {totals.map((t) => (
        <div key={t.currency}>
          <AmountDisplay amount={t.amount} currency={t.currency} />
          {!t.complete && <PartialBadge />}
        </div>
      ))}
    </>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  account?: Account
  onClose: () => void
  onSubmit: (input: CreateAccountInput | UpdateAccountInput) => Promise<void>
  // Edit mode only: the account's Delete, kept out of the row (#657).
  onDelete?: () => Promise<void>
}

// Single form used for both create and edit. In edit mode, PII access is
// available inline (PIIPanel fetches on-demand).
function AccountFormModal({ mode, account, onClose, onSubmit, onDelete }: FormProps) {
  const [name, setName] = useState(account?.name ?? '')
  const [institution, setInstitution] = useState(account?.institution_slug ?? '')
  const [accountType, setAccountType] = useState<AccountType>(account?.account_type ?? 'checking')
  const [currency, setCurrency] = useState(account?.currency ?? 'USD')
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
    <ModalShell aria-label={mode === 'create' ? 'Add account' : `Edit "${account?.name}"`}>
      <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
        {mode === 'create' ? 'Add account' : `Edit "${account?.name}"`}
      </div>
      <div className="space-y-3 px-5 py-4">
        {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        <Field label="Name">
          <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Institution">
          <input className={inputClass} value={institution} onChange={(e) => setInstitution(e.target.value)} placeholder="e.g. Chase" />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Type">
            <select className={inputClass} value={accountType} onChange={(e) => setAccountType(e.target.value as AccountType)}>
              {ACCOUNT_TYPES.map((t) => (
                <option key={t} value={t}>{accountTypeLabel(t)}</option>
              ))}
            </select>
          </Field>
          <Field label="Currency">
            <input className={inputClass} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
          </Field>
        </div>
        {/* No balance field: balance is derived from positions × prices
            (ADR-0013) — to change it, record a transaction or trade. */}
        <Field label="Last 4 (optional)">
          <input className={inputClass} value={lastFour ?? ''} maxLength={4} onChange={(e) => setLastFour(e.target.value)} />
        </Field>
        <label className="flex items-center gap-2 text-sm text-gray-700">
          <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
          Active
        </label>
        {mode === 'edit' && account && <PIIPanel accountID={account.id} />}
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3">
        {onDelete && (
          <button
            type="button"
            onClick={() => {
              void onDelete().catch((err) => setError(errMsg(err)))
            }}
            disabled={submitting}
            className="mr-auto inline-flex min-h-10 items-center gap-1.5 rounded-md px-2 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50"
          >
            <Trash2 size={14} aria-hidden="true" /> Delete account
          </button>
        )}
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
    </ModalShell>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'
const iconButtonClass = 'inline-flex h-10 w-10 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-indigo-700'

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
