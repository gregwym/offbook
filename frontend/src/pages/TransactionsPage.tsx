import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight, Eye, Plus, Sparkles, Trash2, Upload, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { AmountDisplay } from '../components/AmountDisplay'
import { DateDisplay } from '../components/DateDisplay'
import { ImportTransactionsModal } from '../components/ImportTransactionsModal'
import { RuleFormModal, type RuleFormDefaults } from '../components/RuleFormModal'
import { listTransactions } from '../api/transactions'
import { useAccountsStore } from '../store/accountsStore'
import { useAssetsStore } from '../store/assetsStore'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import { useTransactionsStore } from '../store/transactionsStore'
import { useDebounce } from '../hooks/useDebounce'
import type { Account } from '../types/account'
import type { Asset } from '../types/asset'
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
  const { assets, fetch: fetchAssets } = useAssetsStore()
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
  const [importing, setImporting] = useState(false)
  // "Needs review" filter chip + banner. needsReview drives the store
  // filter (categorization_method=plaid_default). reviewCount is a
  // separate query so the banner can announce a number independent of
  // whatever filter the user is currently looking at.
  const [needsReview, setNeedsReview] = useState<boolean>(
    filter.categorization_method === 'plaid_default',
  )
  const [reviewCount, setReviewCount] = useState<number | null>(null)
  // bannerDismissed mirrors localStorage by key. We let it lag behind a
  // sync-driven key change for one frame, but the showBanner logic also
  // checks against the live key, so the user never sees a stale banner.
  const [dismissedKey, setDismissedKey] = useState<string | null>(null)

  useEffect(() => {
    void fetchAccounts()
    void fetchAssets()
    void fetchCategories()
    void fetchRules()
    void fetch()
  }, [fetch, fetchAccounts, fetchAssets, fetchCategories, fetchRules])

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
      categorization_method: needsReview ? 'plaid_default' : undefined,
      from: from === '' ? undefined : from,
      to: to === '' ? undefined : to,
      search: search === '' ? undefined : search,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountID, categoryID, from, to, search, needsReview])

  // Recent-import signal: the most recent last_synced_at across all
  // Plaid-linked accounts. The banner key is built off this so a new
  // sync (= newer timestamp) re-shows the banner even if the user
  // dismissed an earlier one.
  const recentSyncAt = useMemo(() => {
    let max: string | null = null
    for (const a of accounts) {
      if (!a.last_synced_at) continue
      if (max == null || a.last_synced_at > max) max = a.last_synced_at
    }
    return max
  }, [accounts])

  const bannerKey = recentSyncAt ? `txn-review-banner:${recentSyncAt}` : null

  // Pull the count of rows that would land in the "Needs review" filter,
  // independently from the main list (which honors all current filters).
  // limit=1 keeps the fetch cheap — we only need `total`.
  useEffect(() => {
    let cancelled = false
    void listTransactions({ categorization_method: 'plaid_default', limit: 1 })
      .then((r) => { if (!cancelled) setReviewCount(r.total) })
      .catch(() => { /* swallow — banner just won't surface */ })
    return () => { cancelled = true }
  }, [recentSyncAt])

  // Dismissal is persisted in localStorage keyed by bannerKey, so a newer
  // sync (newer key) re-shows the banner without needing reset state. We
  // read once per key change via useMemo (pure derivation); the local
  // `dismissedKey` state covers the in-session click without round-tripping
  // through storage.
  const storedDismissedKey = useMemo(() => {
    if (!bannerKey) return null
    try {
      return window.localStorage.getItem(bannerKey) === '1' ? bannerKey : null
    } catch {
      return null
    }
  }, [bannerKey])
  const bannerDismissed = dismissedKey === bannerKey || storedDismissedKey === bannerKey

  const dismissBanner = () => {
    if (!bannerKey) return
    setDismissedKey(bannerKey)
    try { window.localStorage.setItem(bannerKey, '1') } catch { /* private mode etc. */ }
  }

  // Capture "now" at mount via lazy state init. React's purity rule
  // forbids Date.now() in render or useMemo, and pure state inits sidestep
  // that; coarse 24h window means we don't need a ticker.
  const [nowAtMount] = useState(() => Date.now())
  const RECENT_WINDOW_MS = 24 * 60 * 60 * 1000
  const recentlySynced =
    recentSyncAt != null && nowAtMount - new Date(recentSyncAt).getTime() < RECENT_WINDOW_MS

  const showBanner =
    !needsReview &&
    !bannerDismissed &&
    reviewCount != null && reviewCount > 0 &&
    recentlySynced

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const accountByID = useMemo(() => mapByID(accounts), [accounts])
  const assetByID = useMemo(() => mapByID(assets), [assets])
  const categoryByID = useMemo(() => mapByID(categories), [categories])
  const transactionByID = useMemo(() => mapByID(transactions), [transactions])
  // partnerSkip: ids that render inside a paired-trade row (so we don't
  // also render them as a stand-alone row). Pairing is detected when two
  // visible legs reference each other via transfer_pair_id AND share an
  // account_id — cross-account transfers stay as two separate rows.
  // The lower id is treated as the primary leg.
  const partnerSkip = useMemo(() => {
    const skip = new Set<number>()
    for (const t of transactions) {
      const partnerID = t.transfer_pair_id ?? null
      if (partnerID == null) continue
      const partner = transactionByID.get(partnerID)
      if (!partner) continue
      if (partner.account_id !== t.account_id) continue
      // Skip the higher-id leg; render the lower-id leg as primary.
      const higher = t.id > partner.id ? t.id : partner.id
      skip.add(higher)
    }
    return skip
  }, [transactions, transactionByID])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Transactions</h1>
          <p className="mt-1 text-sm text-gray-500">{total} total</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setImporting(true)}
            className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            <Upload size={16} /> Import CSV
          </button>
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> Add transaction
          </button>
        </div>
      </div>

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
      )}

      {showBanner && (
        <div className="mt-4 flex items-start justify-between gap-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          <div className="flex items-start gap-2">
            <Eye size={16} className="mt-0.5 shrink-0" />
            <span>
              {reviewCount} row{reviewCount === 1 ? '' : 's'} need a category review after the recent sync —{' '}
              <button
                type="button"
                onClick={() => setNeedsReview(true)}
                className="underline hover:text-amber-700"
              >
                show needs review
              </button>
              .
            </span>
          </div>
          <button
            type="button"
            onClick={dismissBanner}
            aria-label="Dismiss banner"
            className="text-amber-700 hover:text-amber-900"
          >
            <X size={14} />
          </button>
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => setNeedsReview((v) => !v)}
          className={[
            'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium',
            needsReview
              ? 'border-amber-400 bg-amber-50 text-amber-800'
              : 'border-gray-300 bg-white text-gray-700 hover:bg-gray-50',
          ].join(' ')}
        >
          <Eye size={12} />
          Needs review
          {reviewCount != null && reviewCount > 0 && (
            <span className={needsReview ? 'text-amber-700' : 'text-gray-500'}>· {reviewCount}</span>
          )}
        </button>
      </div>

      <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-5">
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

      <div className="mt-4 overflow-x-auto rounded-lg border border-gray-200 bg-white">
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
            {transactions.map((t) => {
              if (partnerSkip.has(t.id)) return null
              const partnerID = t.transfer_pair_id ?? null
              const partner = partnerID != null ? transactionByID.get(partnerID) : undefined
              const account = t.account_id ? accountByID.get(t.account_id) : undefined
              if (partner && partner.account_id === t.account_id) {
                // Both legs visible AND same account → collapse into a
                // single trade row, expandable to show both legs.
                return (
                  <PairedTradeRow
                    key={t.id}
                    legA={t}
                    legB={partner}
                    account={account}
                    assetByID={assetByID}
                    categories={categories}
                    categoryByID={categoryByID}
                    onCategoryChange={(legID, catID) => { void setCategory(legID, catID) }}
                    onCreateRule={openRuleFromTxn}
                    onDelete={async (legID) => {
                      if (window.confirm('Delete this trade leg?')) {
                        await remove(legID)
                      }
                    }}
                  />
                )
              }
              return (
                <TransactionRow
                  key={t.id}
                  tx={t}
                  account={account}
                  asset={assetByID.get(t.asset_id)}
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
              )
            })}
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

      {importing && (
        <ImportTransactionsModal
          accounts={accounts}
          onClose={() => setImporting(false)}
          onImported={() => { void fetch() }}
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
  asset?: Asset
  categories: Category[]
  currentCategory?: Category
  onCategoryChange: (catID: number | null) => void
  onCreateRule: () => void
  onDelete: () => Promise<void>
  // Optional indent for rows rendered as legs of an expanded paired
  // trade. Keeps the visual hierarchy clear.
  indented?: boolean
}

function TransactionRow({ tx, account, asset, categories, currentCategory, onCategoryChange, onCreateRule, onDelete, indented }: RowProps) {
  // Per ADR-0013, the unit on a transaction is asset_id. Display currency
  // = asset.symbol when available; fall back to the parent account's
  // currency for older rows that haven't been backfilled.
  const currency = asset?.symbol ?? account?.currency ?? 'USD'
  return (
    <tr className="hover:bg-gray-50">
      <td className={`px-3 py-2 text-gray-700 ${indented ? 'pl-8' : ''}`}><DateDisplay value={tx.transaction_date} /></td>
      <td className="px-3 py-2 text-gray-900">{tx.description ?? '—'}</td>
      <td className="px-3 py-2 text-gray-700">{tx.merchant_name ?? '—'}</td>
      <td className="px-3 py-2 text-right">
        <AmountDisplay amount={tx.amount} currency={currency} signed />
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

type PairedTradeRowProps = {
  legA: Transaction
  legB: Transaction
  account?: Account
  assetByID: Map<number, Asset>
  categories: Category[]
  categoryByID: Map<number, Category>
  onCategoryChange: (legID: number, catID: number | null) => void
  onCreateRule: (tx: Transaction) => void
  onDelete: (legID: number) => Promise<void>
}

// PairedTradeRow renders two paired transaction rows (a trade) as a
// single collapsed line: "Bought N AAPL @ $P · -$cash". Expanding shows
// both legs as full standalone rows. The collapsed view is read-only
// (no per-leg category edits) — for those, expand.
function PairedTradeRow({
  legA,
  legB,
  account,
  assetByID,
  categories,
  categoryByID,
  onCategoryChange,
  onCreateRule,
  onDelete,
}: PairedTradeRowProps) {
  const [expanded, setExpanded] = useState(false)

  // Classify legs: the cash leg's asset matches the account's primary
  // quote asset. If the account is missing (rare — partner from another
  // page), fall back to "leg with the smaller asset_id wins as cash" —
  // good enough since we still display both on expand.
  const cashAssetID = account?.primary_quote_asset_id
  let cashLeg: Transaction
  let securityLeg: Transaction
  if (cashAssetID != null && legA.asset_id === cashAssetID) {
    cashLeg = legA
    securityLeg = legB
  } else if (cashAssetID != null && legB.asset_id === cashAssetID) {
    cashLeg = legB
    securityLeg = legA
  } else {
    // Fallback: the leg whose absolute amount value is "round" cash —
    // we can't reliably guess, so just pick legA as security.
    securityLeg = legA
    cashLeg = legB
  }

  const securityAsset = assetByID.get(securityLeg.asset_id)
  const cashAsset = assetByID.get(cashLeg.asset_id)
  const securityCurrency = securityAsset?.symbol ?? '—'
  const cashCurrency = cashAsset?.symbol ?? account?.currency ?? 'USD'

  return (
    <>
      <tr className="hover:bg-gray-50">
        <td className="px-3 py-2 text-gray-700">
          <DateDisplay value={securityLeg.transaction_date} />
        </td>
        <td className="px-3 py-2 text-gray-900">
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="mr-1 inline-flex items-center text-gray-400 hover:text-gray-700"
            aria-label={expanded ? 'Collapse trade' : 'Expand trade'}
            title={expanded ? 'Collapse legs' : 'Show both legs'}
          >
            {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </button>
          {securityLeg.description ?? '—'}
          <span className="ml-2 inline-flex items-center rounded bg-indigo-50 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-indigo-700">
            trade
          </span>
        </td>
        <td className="px-3 py-2 text-gray-400">—</td>
        <td className="px-3 py-2 text-right">
          <div className="tabular-nums">
            <AmountDisplay amount={securityLeg.amount} currency={securityCurrency} signed />
          </div>
          <div className="tabular-nums text-xs text-gray-500">
            <AmountDisplay amount={cashLeg.amount} currency={cashCurrency} signed />
          </div>
        </td>
        <td className="px-3 py-2 text-gray-700">{account?.name ?? `#${securityLeg.account_id}`}</td>
        <td className="px-3 py-2 text-gray-400 italic text-xs">expand to edit</td>
        <td className="px-3 py-2"></td>
      </tr>
      {expanded && (
        <>
          <TransactionRow
            tx={securityLeg}
            account={account}
            asset={securityAsset}
            categories={categories}
            currentCategory={securityLeg.category_id ? categoryByID.get(securityLeg.category_id) : undefined}
            onCategoryChange={(catID) => onCategoryChange(securityLeg.id, catID)}
            onCreateRule={() => onCreateRule(securityLeg)}
            onDelete={() => onDelete(securityLeg.id)}
            indented
          />
          <TransactionRow
            tx={cashLeg}
            account={account}
            asset={cashAsset}
            categories={categories}
            currentCategory={cashLeg.category_id ? categoryByID.get(cashLeg.category_id) : undefined}
            onCategoryChange={(catID) => onCategoryChange(cashLeg.id, catID)}
            onCreateRule={() => onCreateRule(cashLeg)}
            onDelete={() => onDelete(cashLeg.id)}
            indented
          />
        </>
      )}
    </>
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
            <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
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
          <Field label="Amount (negative = spending)">
            <input className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
          </Field>
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
