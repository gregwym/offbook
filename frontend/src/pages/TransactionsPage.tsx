import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Plus, Sparkles, Trash2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import { AmountDisplay } from '../components/AmountDisplay'
import { DateDisplay } from '../components/DateDisplay'
import { RuleFormModal, type RuleFormDefaults } from '../components/RuleFormModal'
import { useAccountsStore } from '../store/accountsStore'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import { useTransactionsStore } from '../store/transactionsStore'
import { useDebounce } from '../hooks/useDebounce'
import type { Account } from '../types/account'
import type { Category } from '../types/category'
import type { CreateRuleInput } from '../types/categorizationRule'
import type { CreateTransactionInput, Transaction, TransactionSource } from '../types/transaction'
import { TRANSACTION_SOURCES } from '../types/transaction'

export function TransactionsPage() {
  const {
    transactions, total, loading, error,
    filter, page, pageSize,
    fetch, setFilter, setPage, create, setCategory, remove,
  } = useTransactionsStore()
  const { accounts, fetch: fetchAccounts } = useAccountsStore()
  const { categories, fetch: fetchCategories } = useCategoriesStore()
  const {
    rules,
    fetch: fetchRules,
    create: createRule,
    apply: applyRules,
  } = useRulesStore()
  const [ruleSeed, setRuleSeed] = useState<RuleFormDefaults | null>(null)
  // After a successful rule create from a txn, surface a toast with two
  // actions: jump to /rules, or re-apply to existing transactions.
  const [createdRuleToast, setCreatedRuleToast] = useState<string | null>(null)
  const [applyToastBusy, setApplyToastBusy] = useState(false)

  // Local filter inputs — synced into the store via setFilter on change.
  const [accountID, setAccountID] = useState<string>(String(filter.account_id ?? ''))
  const [categoryID, setCategoryID] = useState<string>(
    filter.category_id === undefined ? '' : filter.category_id === null ? 'null' : String(filter.category_id),
  )
  const [from, setFrom] = useState<string>(filter.from ?? '')
  const [to, setTo] = useState<string>(filter.to ?? '')
  const [searchInput, setSearchInput] = useState<string>(filter.search ?? '')
  const search = useDebounce(searchInput, 300)
  const [adding, setAdding] = useState(false)

  useEffect(() => {
    void fetchAccounts()
    void fetchCategories()
    void fetchRules()
    void fetch()
  }, [fetch, fetchAccounts, fetchCategories, fetchRules])

  // Same priority heuristic as RulesPage — new rules outrank existing ones
  // by default so the "create from this txn" shortcut wins immediately on
  // the next sync.
  const nextPriority = useMemo(() => {
    if (rules.length === 0) return 10
    return rules.reduce((m, r) => Math.max(m, r.priority), 0) + 10
  }, [rules])

  const openRuleFromTxn = (t: Transaction) => {
    const pattern =
      (t.merchant_name && t.merchant_name.trim()) ||
      (t.description_clean && t.description_clean.trim()) ||
      (t.description && t.description.trim()) ||
      ''
    setRuleSeed({
      pattern,
      match_type: 'contains',
      category_id: t.category_id ?? undefined,
      priority: nextPriority,
    })
  }

  // Propagate the typed inputs into the store filter. setFilter resets page to 0.
  useEffect(() => {
    setFilter({
      account_id: accountID === '' ? undefined : Number(accountID),
      category_id: categoryID === '' ? undefined : categoryID === 'null' ? null : Number(categoryID),
      from: from === '' ? undefined : from,
      to: to === '' ? undefined : to,
      search: search === '' ? undefined : search,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountID, categoryID, from, to, search])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const accountByID = useMemo(() => mapByID(accounts), [accounts])
  const categoryByID = useMemo(() => mapByID(categories), [categories])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Transactions</h1>
          <p className="mt-1 text-sm text-gray-500">{total} total</p>
        </div>
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> Add transaction
        </button>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
      )}

      <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-5">
        <Field label="Account">
          <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
            <option value="">All</option>
            {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
        </Field>
        <Field label="Category">
          <select className={inputClass} value={categoryID} onChange={(e) => setCategoryID(e.target.value)}>
            <option value="">All</option>
            <option value="null">Uncategorized</option>
            {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </Field>
        <Field label="From">
          <input type="date" className={inputClass} value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label="To">
          <input type="date" className={inputClass} value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
        <Field label="Search">
          <input className={inputClass} placeholder="description or merchant" value={searchInput} onChange={(e) => setSearchInput(e.target.value)} />
        </Field>
      </div>

      <div className="mt-4 overflow-hidden rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="px-3 py-2 text-left">Date</th>
              <th className="px-3 py-2 text-left">Description</th>
              <th className="px-3 py-2 text-left">Merchant</th>
              <th className="px-3 py-2 text-right">Amount</th>
              <th className="px-3 py-2 text-left">Account</th>
              <th className="px-3 py-2 text-left">Category</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {loading && transactions.length === 0 && (
              <tr><td colSpan={7} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && transactions.length === 0 && (
              <tr><td colSpan={7} className="px-4 py-6 text-center text-gray-400">No transactions match these filters.</td></tr>
            )}
            {transactions.map((t) => (
              <TransactionRow
                key={t.id}
                tx={t}
                account={t.account_id ? accountByID.get(t.account_id) : undefined}
                categories={categories}
                currentCategory={t.category_id ? categoryByID.get(t.category_id) : undefined}
                onCategoryChange={(catID) => { void setCategory(t.id, catID) }}
                onCreateRule={() => openRuleFromTxn(t)}
                onDelete={async () => {
                  if (window.confirm('Delete this transaction?')) {
                    await remove(t.id)
                  }
                }}
              />
            ))}
          </tbody>
        </table>
      </div>

      <div className="mt-3 flex items-center justify-end gap-2 text-sm">
        <button
          type="button"
          onClick={() => setPage(page - 1)}
          disabled={page === 0}
          className="rounded border border-gray-300 px-2 py-1 disabled:opacity-50"
        >Prev</button>
        <span className="text-gray-600">Page {page + 1} / {totalPages}</span>
        <button
          type="button"
          onClick={() => setPage(page + 1)}
          disabled={page + 1 >= totalPages}
          className="rounded border border-gray-300 px-2 py-1 disabled:opacity-50"
        >Next</button>
      </div>

      {adding && (
        <AddTransactionModal
          accounts={accounts}
          categories={categories}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input)
            setAdding(false)
          }}
        />
      )}

      {ruleSeed && (
        <RuleFormModal
          mode="create"
          categories={categories}
          defaults={ruleSeed}
          onClose={() => setRuleSeed(null)}
          onSubmit={async (input) => {
            await createRule(input as CreateRuleInput)
            setRuleSeed(null)
            setCreatedRuleToast(`Rule "${(input as CreateRuleInput).pattern}" created.`)
          }}
        />
      )}

      {createdRuleToast && (
        <div className="fixed bottom-4 right-4 z-30 flex max-w-md items-start gap-3 rounded-md border border-emerald-200 bg-white px-4 py-3 text-sm shadow-lg">
          <Sparkles size={18} className="mt-0.5 text-emerald-600" />
          <div className="flex-1">
            <div className="font-medium text-gray-900">{createdRuleToast}</div>
            <div className="mt-2 flex gap-2">
              <Link
                to="/rules"
                className="rounded-md border border-gray-300 px-2 py-1 text-xs text-gray-700 hover:bg-gray-50"
                onClick={() => setCreatedRuleToast(null)}
              >
                View rules
              </Link>
              <button
                type="button"
                disabled={applyToastBusy}
                onClick={async () => {
                  setApplyToastBusy(true)
                  try {
                    const result = await applyRules()
                    setCreatedRuleToast(
                      `Re-applied: scanned ${result.scanned}, updated ${result.updated}` +
                        (result.skipped_manual > 0 ? `, skipped ${result.skipped_manual} manual.` : '.'),
                    )
                    // Refresh the transactions list so the new categorization shows up.
                    await fetch()
                  } catch {
                    // ignore — error toast handled by rules store
                  } finally {
                    setApplyToastBusy(false)
                  }
                }}
                className="rounded-md bg-emerald-600 px-2 py-1 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
              >
                {applyToastBusy ? 'Applying…' : 'Apply to existing transactions'}
              </button>
            </div>
          </div>
          <button
            type="button"
            onClick={() => setCreatedRuleToast(null)}
            className="ml-1 text-gray-400 hover:text-gray-700"
            aria-label="Dismiss"
          >×</button>
        </div>
      )}
    </div>
  )
}

type RowProps = {
  tx: Transaction
  account?: Account
  categories: Category[]
  currentCategory?: Category
  onCategoryChange: (catID: number | null) => void
  onCreateRule: () => void
  onDelete: () => Promise<void>
}

function TransactionRow({ tx, account, categories, currentCategory, onCategoryChange, onCreateRule, onDelete }: RowProps) {
  return (
    <tr className="hover:bg-gray-50">
      <td className="px-3 py-2 text-gray-700"><DateDisplay value={tx.transaction_date} /></td>
      <td className="px-3 py-2 text-gray-900">{tx.description ?? '—'}</td>
      <td className="px-3 py-2 text-gray-700">{tx.merchant_name ?? '—'}</td>
      <td className="px-3 py-2 text-right">
        <AmountDisplay amount={tx.amount} currency={tx.currency} signed />
      </td>
      <td className="px-3 py-2 text-gray-700">{account?.name ?? `#${tx.account_id}`}</td>
      <td className="px-3 py-2">
        <select
          value={currentCategory ? currentCategory.id : ''}
          onChange={(e) => {
            const v = e.target.value
            onCategoryChange(v === '' ? null : Number(v))
          }}
          className="rounded border border-gray-300 px-1.5 py-1 text-sm"
        >
          <option value="">Uncategorized</option>
          {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
      </td>
      <td className="px-3 py-2 text-right">
        <button
          type="button"
          onClick={onCreateRule}
          className="mr-2 text-gray-500 hover:text-indigo-700"
          aria-label="Create rule from this transaction"
          title="Create rule from this transaction"
        >
          <Sparkles size={16} />
        </button>
        <button
          type="button"
          onClick={onDelete}
          className="text-gray-500 hover:text-red-700"
          aria-label="Delete"
        >
          <Trash2 size={16} />
        </button>
      </td>
    </tr>
  )
}

type AddProps = {
  accounts: Account[]
  categories: Category[]
  onClose: () => void
  onSubmit: (input: CreateTransactionInput) => Promise<void>
}

function AddTransactionModal({ accounts, categories, onClose, onSubmit }: AddProps) {
  const [accountID, setAccountID] = useState<string>(accounts[0] ? String(accounts[0].id) : '')
  const [categoryID, setCategoryID] = useState<string>('')
  const [amount, setAmount] = useState('0.00')
  const [currency, setCurrency] = useState(accounts[0]?.currency ?? 'USD')
  const [description, setDescription] = useState('')
  const [merchant, setMerchant] = useState('')
  const [date, setDate] = useState<string>(new Date().toISOString().slice(0, 10))
  const [source, setSource] = useState<TransactionSource>('manual')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    if (!accountID) {
      setError('Account is required.')
      return
    }
    if (!amount.trim()) {
      setError('Amount is required.')
      return
    }
    if (!date) {
      setError('Date is required.')
      return
    }
    setError(null)
    setSubmitting(true)
    try {
      await onSubmit({
        account_id: Number(accountID),
        category_id: categoryID === '' ? null : Number(categoryID),
        amount,
        currency: currency.toUpperCase(),
        description: description.trim() || null,
        merchant_name: merchant.trim() || null,
        transaction_date: date,
        source,
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
        <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">Add transaction</div>
        <div className="space-y-3 px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Account">
            <select className={inputClass} value={accountID} onChange={(e) => {
              setAccountID(e.target.value)
              const acc = accounts.find((a) => String(a.id) === e.target.value)
              if (acc) setCurrency(acc.currency)
            }}>
              <option value="">(none)</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </Field>
          <Field label="Category">
            <select className={inputClass} value={categoryID} onChange={(e) => setCategoryID(e.target.value)}>
              <option value="">Uncategorized</option>
              {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Amount (negative = spending)">
              <input className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
            </Field>
            <Field label="Currency">
              <input className={inputClass} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
            </Field>
          </div>
          <Field label="Description">
            <input className={inputClass} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <Field label="Merchant">
            <input className={inputClass} value={merchant} onChange={(e) => setMerchant(e.target.value)} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Date">
              <input type="date" className={inputClass} value={date} onChange={(e) => setDate(e.target.value)} />
            </Field>
            <Field label="Source">
              <select className={inputClass} value={source} onChange={(e) => setSource(e.target.value as TransactionSource)}>
                {TRANSACTION_SOURCES.map((s) => <option key={s} value={s}>{s}</option>)}
              </select>
            </Field>
          </div>
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : 'Create'}
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

function mapByID<T extends { id: number }>(items: T[]): Map<number, T> {
  const m = new Map<number, T>()
  for (const item of items) m.set(item.id, item)
  return m
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
