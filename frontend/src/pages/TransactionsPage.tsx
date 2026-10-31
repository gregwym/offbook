import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowRightLeft, Check, CheckCheck, ChevronDown, ChevronRight, Eye, EyeOff, Pencil, Plus, Scissors, SlidersHorizontal, Sparkles, Tag, Trash2, Undo2, Upload, X } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { AmountDisplay } from '../components/AmountDisplay'
import { DateDisplay } from '../components/DateDisplay'
import { ImportTransactionsModal } from '../components/ImportTransactionsModal'
import { SplitTransactionModal } from '../components/SplitTransactionModal'
import { TagEditorModal } from '../components/TagEditorModal'
import { EditDetailsModal } from '../components/EditDetailsModal'
import { RuleFormModal, type RuleFormDefaults } from '../components/RuleFormModal'
import { TransferMatchesModal } from '../components/TransferMatchesModal'
import { listTransactions, listTransactionTags } from '../api/transactions'
import { listTransferCandidates, unlinkTransfer } from '../api/transfers'
import type { TransferCandidate } from '../api/transfers'
import { useAccountsStore } from '../store/accountsStore'
import { useAssetsStore } from '../store/assetsStore'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import { useTransactionsStore } from '../store/transactionsStore'
import { useDebounce } from '../hooks/useDebounce'
import { accountTypeLabel, type Account } from '../types/account'
import type { Asset } from '../types/asset'
import type { Category } from '../types/category'
import type { CreateRuleInput } from '../types/categorizationRule'
import type { BulkEditAction, BulkEditPrior, CreateTransactionInput, TagCount, Transaction, TransactionSource } from '../types/transaction'
import { TRANSACTION_SOURCES } from '../types/transaction'
import { ModalShell } from '../components/ModalShell'
import { PartialBadge } from '../components/PartialBadge'
import { AccountBalance } from '../components/AccountBalance'
import { UpdateBalanceModal } from '../components/UpdateBalanceModal'
import { canUpdateBalance } from '../lib/accountActions'
import { RowActionsMenu, type RowAction } from '../components/RowActionsMenu'
import { txDisplayName, txSecondaryDescription } from '../lib/txName'
import { NEW_CATEGORY, NewCategoryDialog, NewCategoryOption } from '../components/NewCategoryDialog'

export function TransactionsPage() {
  const {
    transactions, total, spendTotal, loading, error,
    filter, page, pageSize, reviewPending,
    fetch, setFilter, setPage, create, setCategory, update, remove, markReviewed, bulkEdit, undoBulkEdit,
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
  const [splitting, setSplitting] = useState<Transaction | null>(null)
  // Tags (#544): the row being tagged, and the user's tags for the filter
  // and the editor's suggestions.
  const [tagging, setTagging] = useState<Transaction | null>(null)
  const [editing, setEditing] = useState<Transaction | null>(null)
  const [tagOptions, setTagOptions] = useState<TagCount[]>([])
  // After a successful rule create from a txn, surface a toast with two
  // actions: jump to /rules, or re-apply to existing transactions.
  const [createdRuleToast, setCreatedRuleToast] = useState<string | null>(null)
  const [applyToastBusy, setApplyToastBusy] = useState(false)

  // Local filter inputs — synced into the store via setFilter on change.
  // A spending drill-down link (#571) carries its category and dates in the
  // URL; those win over whatever filter the store remembers.
  const [urlParams, setUrlParams] = useSearchParams()
  const drillIn = urlParams.get('spending') === '1'
  // The account filter lives in the URL (?account_id=N, #657) so an account
  // link from Accounts, a reload or a shared link all land on that account.
  // Without one, the store's remembered account is restored into the URL.
  const urlAccount = urlParams.get('account_id') ?? ''
  const accountID = /^\d+$/.test(urlAccount) ? urlAccount : ''
  const setAccountID = useCallback(
    (next: string) => {
      setUrlParams(
        (prev) => {
          if ((prev.get('account_id') ?? '') === next) return prev
          const out = new URLSearchParams(prev)
          if (next) out.set('account_id', next)
          else out.delete('account_id')
          return out
        },
        { replace: true },
      )
    },
    [setUrlParams],
  )
  useEffect(() => {
    if (!drillIn && !urlParams.has('account_id') && filter.account_id != null) {
      setAccountID(String(filter.account_id))
    }
    // Mount only: restore once, then the URL is the source of truth.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const [categoryID, setCategoryID] = useState<string>(
    drillIn
      ? urlParams.get('category_id') ?? ''
      : filter.category_id === undefined ? '' : filter.category_id === null ? 'null' : String(filter.category_id),
  )
  // A group budget's drill-down also names the subcategories it counts
  // (#596). They belong to the category they came with: picking another
  // category drops them.
  const [subcats] = useState(() => ({
    category: drillIn ? urlParams.get('category_id') ?? '' : '',
    ids: drillIn
      ? (urlParams.get('subcategory_ids') ?? '').split(',').map(Number).filter((n) => Number.isInteger(n) && n > 0)
      : [],
  }))
  const subcategoryIDs = useMemo(
    () => (categoryID !== '' && categoryID === subcats.category ? subcats.ids : []),
    [categoryID, subcats],
  )
  const subcategoryNames = subcategoryIDs
    .map((id) => categories.find((c) => c.id === id)?.name ?? `#${id}`)
    .join(', ')
  const [from, setFrom] = useState<string>(drillIn ? urlParams.get('from') ?? '' : filter.from ?? '')
  const [to, setTo] = useState<string>(drillIn ? urlParams.get('to') ?? '' : filter.to ?? '')
  const [searchInput, setSearchInput] = useState<string>(drillIn ? '' : filter.search ?? '')
  const [tag, setTag] = useState<string>(drillIn ? '' : filter.tag ?? '')
  const search = useDebounce(searchInput, 300)
  const [adding, setAdding] = useState(false)
  const [importing, setImporting] = useState(false)
  const [balancing, setBalancing] = useState<Account | null>(null)
  const filteredAccount = accountID ? accounts.find((a) => String(a.id) === accountID) ?? null : null
  // One account filtered (#675): every row is that account, so the table
  // drops the Account column and the phone meta line drops its suffix.
  const showAccount = !accountID
  const filtersActive = Boolean(accountID || categoryID || from || to || searchInput || tag)
  // Below md only Search is shown up front; Account/Category/From/To sit
  // behind a "Filters" toggle so the list isn't pushed below the fold (#518).
  // The panel starts collapsed even with one of them set: removable chips
  // summarise what is applied instead, so a live filter is never hidden and
  // the list still starts on the first screen (#554). Desktop ignores this
  // and always shows all five inline.
  const moreFilterCount = [accountID, categoryID, from, to, tag].filter(Boolean).length
  const [moreFiltersOpen, setMoreFiltersOpen] = useState(false)
  const moreFilterClass = moreFiltersOpen ? 'block' : 'hidden md:block'
  const activeFilterChips = [
    accountID && {
      key: 'account',
      text: `Account: ${accounts.find((a) => String(a.id) === accountID)?.name ?? accountID}`,
      clear: () => setAccountID(''),
    },
    categoryID && {
      key: 'category',
      text: `Category: ${categoryID === 'null' ? 'Uncategorized' : categories.find((c) => String(c.id) === categoryID)?.name ?? categoryID}${subcategoryNames ? ` + ${subcategoryNames}` : ''}`,
      clear: () => setCategoryID(''),
    },
    from && { key: 'from', text: `From: ${from}`, clear: () => setFrom('') },
    to && { key: 'to', text: `To: ${to}`, clear: () => setTo('') },
    tag && { key: 'tag', text: `Tag: ${tag}`, clear: () => setTag('') },
  ].filter((c): c is { key: string; text: string; clear: () => void } => Boolean(c))
  const clearFilters = () => {
    setAccountID('')
    setCategoryID('')
    setFrom('')
    setTo('')
    setSearchInput('')
    setTag('')
  }
  // "Needs review" filter chip + banner (#424). needsReview drives the store
  // filter (reviewed=false) — explicit review state, independent of how a row
  // was categorized. reviewCount is a separate query so the chip/banner can
  // announce a number independent of whatever filter the user is looking at.
  // The chip lives in the URL (?review=1) so a reload or shared link keeps the
  // inbox open instead of silently dropping back to the full list.
  const [searchParams, setSearchParams] = useSearchParams()
  const needsReview = searchParams.get('review') === '1'
  const setNeedsReview = useCallback(
    (next: boolean | ((v: boolean) => boolean)) => {
      setSearchParams(
        (prev) => {
          const cur = prev.get('review') === '1'
          const val = typeof next === 'function' ? next(cur) : next
          const out = new URLSearchParams(prev)
          if (val) out.set('review', '1')
          else out.delete('review')
          return out
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )
  const [reviewCount, setReviewCount] = useState<number | null>(null)
  // Spending drill-down (#571): ?spending=1 lists only the rows the spending
  // figures count, and the banner shows their total — the number the user
  // clicked on Insights or Budgets. Category and dates stay in the URL so a
  // reload or shared link keeps the same view.
  const spending = searchParams.get('spending') === '1'
  const merchant = spending ? searchParams.get('merchant') ?? '' : ''
  useEffect(() => {
    if (!spending) return
    setSearchParams(
      (prev) => {
        const out = new URLSearchParams(prev)
        const want: Record<string, string> = { category_id: categoryID, subcategory_ids: subcategoryIDs.join(','), from, to }
        let changed = false
        for (const [k, v] of Object.entries(want)) {
          if ((prev.get(k) ?? '') === v) continue
          changed = true
          if (v) out.set(k, v)
          else out.delete(k)
        }
        return changed ? out : prev
      },
      { replace: true },
    )
  }, [spending, categoryID, subcategoryIDs, from, to, setSearchParams])
  const exitSpending = () => {
    setSearchParams(
      (prev) => {
        const out = new URLSearchParams(prev)
        for (const k of ['spending', 'merchant', 'category_id', 'subcategory_ids', 'from', 'to']) out.delete(k)
        return out
      },
      { replace: true },
    )
  }
  // Clear the lens's category/dates once the URL has actually left it. Doing
  // it in exitSpending would race the navigation (router updates land in a
  // transition), and the sync effect above would write the old URL back.
  const wasSpending = useRef(spending)
  useEffect(() => {
    if (wasSpending.current && !spending) {
      setCategoryID('')
      setFrom('')
      setTo('')
    }
    wasSpending.current = spending
  }, [spending])
  const spendingLabel = merchant
    ? merchant
    : categoryID === 'null'
      ? 'Uncategorized'
      : categoryID
        ? `${categories.find((c) => String(c.id) === categoryID)?.name ?? 'Category'}${subcategoryNames ? ` (incl. ${subcategoryNames})` : ''}`
        : 'All categories'
  // Row selection for bulk review, plus the last review action so an
  // accidental click can be undone without a reload.
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [reviewToast, setReviewToast] = useState<{ ids: number[]; reviewed: boolean; message: string } | null>(null)
  const [reviewBusy, setReviewBusy] = useState(false)
  // Last bulk category/hide edit (#567); `undo` is null once there is
  // nothing left to revert (an undo result, or an edit that changed nothing).
  const [bulkToast, setBulkToast] = useState<{ message: string; undo: BulkEditPrior[] | null } | null>(null)
  // Transfer-match proposals (#371) — a separate review queue from "needs
  // review" categorization. Recomputed fresh on mount/after each sync, same
  // as reviewCount above; nothing links until the user confirms a pair in
  // the modal.
  const [transferCandidates, setTransferCandidates] = useState<TransferCandidate[]>([])
  const [reviewingTransfers, setReviewingTransfers] = useState(false)
  // bannerDismissed mirrors localStorage by key. We let it lag behind a
  // sync-driven key change for one frame, but the showBanner logic also
  // checks against the live key, so the user never sees a stale banner.
  const [dismissedKey, setDismissedKey] = useState<string | null>(null)

  const refreshTransferCandidates = () => {
    void listTransferCandidates()
      .then((r) => setTransferCandidates(r.items))
      .catch(() => { /* swallow — the banner/modal just won't surface */ })
  }

  const refreshTags = useCallback(() => {
    void listTransactionTags()
      .then(setTagOptions)
      .catch(() => { /* swallow — the tag filter just lists nothing */ })
  }, [])

  useEffect(() => {
    refreshTags()
    void fetchAccounts()
    void fetchAssets()
    void fetchCategories()
    void fetchRules()
    void fetch()
    refreshTransferCandidates()
  }, [fetch, fetchAccounts, fetchAssets, fetchCategories, fetchRules, refreshTags])

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
      reviewed: needsReview ? false : undefined,
      from: from === '' ? undefined : from,
      to: to === '' ? undefined : to,
      search: search === '' ? undefined : search,
      tag: tag === '' ? undefined : tag,
      spending: spending || undefined,
      merchant: merchant === '' ? undefined : merchant,
      subcategory_ids: spending && subcategoryIDs.length ? subcategoryIDs : undefined,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accountID, categoryID, subcategoryIDs, from, to, search, tag, needsReview, spending, merchant])

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

  // Pull the count of rows that would land in the "Needs review" filter.
  // It applies every active list filter (account, category, dates, search,
  // tag, spending drill-down) with reviewed=false, so the chip's number is
  // exactly what pressing it shows (#675); with no filter it counts every
  // account. limit=1 keeps the fetch cheap — we only need `total`.
  // Only the latest request may set the count: the unfiltered first-render
  // query can resolve after the filtered one and would otherwise overwrite
  // it with the all-accounts number.
  // The request reads the filter on screen *when it is sent* (a ref), not
  // the one captured when an action started: a review or bulk edit that
  // resolves after the user switched accounts must count the new account,
  // not the old one (#688).
  const countFilter = useMemo(() => ({ ...filter, reviewed: false, limit: 1, offset: 0 }), [filter])
  const countFilterRef = useRef(countFilter)
  countFilterRef.current = countFilter
  const reviewCountSeq = useRef(0)
  const refreshReviewCount = useCallback(() => {
    const seq = ++reviewCountSeq.current
    return listTransactions(countFilterRef.current)
      .then((r) => { if (seq === reviewCountSeq.current) setReviewCount(r.total) })
      .catch(() => { /* swallow — the count just won't surface */ })
  }, [])
  useEffect(() => {
    void refreshReviewCount()
  }, [recentSyncAt, countFilter, refreshReviewCount])

  // Review / undo. The store updates optimistically and reverts on failure;
  // the toast offers a one-click undo for the exact rows just changed.
  const reviewRows = async (ids: number[], reviewed: boolean, offerUndo = true) => {
    if (ids.length === 0) return
    setReviewBusy(true)
    try {
      await markReviewed(ids, reviewed)
      setSelected(new Set())
      setBulkToast(null)
      // Un-reviewing while the inbox is open: those rows aren't in the list
      // (the optimistic update removed them), so reload to bring them back.
      if (!reviewed && needsReview) await fetch()
      setReviewToast(
        offerUndo
          ? { ids, reviewed, message: `${ids.length} transaction${ids.length === 1 ? '' : 's'} ${reviewed ? 'marked reviewed' : 'moved back to review'}.` }
          : null,
      )
    } catch {
      setReviewToast(null) // store.error renders the failure banner
    } finally {
      setReviewBusy(false)
      void refreshReviewCount()
    }
  }

  // Bulk category / hide (#567). One request for all selected rows; the toast
  // offers an undo that restores exactly the rows this edit changed.
  const bulkEditRows = async (ids: number[], action: BulkEditAction, done: (n: number) => string) => {
    if (ids.length === 0) return
    setReviewBusy(true)
    try {
      const res = await bulkEdit(ids, action)
      setSelected(new Set())
      setReviewToast(null)
      const skipped = res.skipped_split > 0
        ? ` ${res.skipped_split} split transaction${res.skipped_split === 1 ? '' : 's'} skipped: set ${res.skipped_split === 1 ? 'its' : 'their'} category on the split lines.`
        : ''
      setBulkToast(
        res.updated === 0
          ? { message: `Nothing to change: the selection already matches.${skipped}`, undo: null }
          : { message: `${done(res.updated)}${skipped}`, undo: res.undo },
      )
    } catch {
      setBulkToast(null) // store.error renders the failure banner
    } finally {
      setReviewBusy(false)
      void refreshReviewCount()
    }
  }

  const undoBulkRows = async (undo: BulkEditPrior[]) => {
    setReviewBusy(true)
    try {
      const res = await undoBulkEdit(undo)
      const kept = res.kept > 0
        ? ` ${res.kept} you changed since ${res.kept === 1 ? 'was' : 'were'} kept as ${res.kept === 1 ? 'it is' : 'they are'}.`
        : ''
      setBulkToast({ message: `Undone: ${res.restored} transaction${res.restored === 1 ? '' : 's'} restored.${kept}`, undo: null })
    } catch {
      setBulkToast(null)
    } finally {
      setReviewBusy(false)
      void refreshReviewCount()
    }
  }

  const plural = (n: number) => `${n} transaction${n === 1 ? '' : 's'}`
  // "+ New category…" in a picker (#581): the dialog creates the category,
  // then `apply` selects it where the user was.
  const [newCategory, setNewCategory] = useState<{ apply: (c: Category) => void; from: HTMLElement } | null>(null)
  const setSelectedCategory = (value: string, from: HTMLElement) => {
    if (value === '') return
    if (value === NEW_CATEGORY) {
      const ids = selectedIDs
      setNewCategory({
        apply: (c) => { void bulkEditRows(ids, { category_id: c.id }, (n) => `${plural(n)} moved to ${c.name}.`) },
        from,
      })
      return
    }
    if (value === 'null') {
      void bulkEditRows(selectedIDs, { clear_category: true }, (n) => `${plural(n)} set to Uncategorized.`)
      return
    }
    const cat = categories.find((c) => c.id === Number(value))
    void bulkEditRows(selectedIDs, { category_id: Number(value) }, (n) => `${plural(n)} moved to ${cat?.name ?? 'the category'}.`)
  }

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
  // Selection only ever applies to rows currently on screen.
  const visibleIDs = useMemo(
    () => transactions.filter((t) => t.kind === 'flow' || t.kind === undefined).map((t) => t.id),
    [transactions],
  )
  const selectedIDs = visibleIDs.filter((id) => selected.has(id))
  const allSelected = visibleIDs.length > 0 && selectedIDs.length === visibleIDs.length
  const selectedRows = transactions.filter((t) => selected.has(t.id) && visibleIDs.includes(t.id))
  const categoryByID = useMemo(() => mapByID(categories), [categories])
  // Transfer legs never count in spending, so "Hide" would change nothing
  // there (#643): bulk Hide leaves them out of the request and the count.
  const hideableSelectedIDs = selectedRows
    .filter((t) => t.hidden_at == null && !excludedAsTransfer(t, t.category_id ? categoryByID.get(t.category_id) : undefined))
    .map((t) => t.id)
  const anySelectedShown = hideableSelectedIDs.length > 0
  const anySelectedHidden = selectedRows.some((t) => t.hidden_at != null)
  const toggleSelected = (id: number) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const accountByID = useMemo(() => mapByID(accounts), [accounts])
  const assetByID = useMemo(() => mapByID(assets), [assets])
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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Transactions</h1>
          <p className="mt-1 text-sm text-gray-500">
            {needsReview ? `${total} to review` : `${total} total`}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setImporting(true)}
            className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            <Upload size={16} /> Import
          </button>
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 whitespace-nowrap rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> Add transaction
          </button>
        </div>
      </div>

      {filteredAccount && (
        <AccountHeader
          account={filteredAccount}
          onUpdateBalance={() => setBalancing(filteredAccount)}
          onShowAll={() => setAccountID('')}
        />
      )}

      {error && (
        <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
      )}

      {spending && (
        <section
          aria-label="Spending drill-down"
          className="mt-4 flex flex-wrap items-start justify-between gap-x-3 gap-y-2 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm text-indigo-900"
        >
          <div className="min-w-0">
            <div className="font-medium">
              Spending · <span className="break-words">{spendingLabel}</span>
            </div>
            <div className="text-indigo-800">
              {from || to ? (
                <>
                  <DateDisplay value={from || null} /> – <DateDisplay value={to || null} /> ·{' '}
                </>
              ) : null}
              {total} transaction{total === 1 ? '' : 's'}
              {spendTotal !== null && (
                <>
                  {' · '}
                  <span data-testid="spend-total" className="font-medium">
                    <AmountDisplay amount={spendTotal} />
                  </span>
                </>
              )}
            </div>
            <div className="mt-0.5 text-xs text-indigo-700">
              Only what counts as spending: transfers and hidden rows are left out, refunds are subtracted, and a split
              row counts its part in this category.
            </div>
          </div>
          <button
            type="button"
            onClick={exitSpending}
            className="shrink-0 rounded-md border border-indigo-300 bg-white px-2.5 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100"
          >
            Show all transactions
          </button>
        </section>
      )}

      {showBanner && (
        <div className="mt-4 flex items-start justify-between gap-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
          <div className="flex items-start gap-2">
            <Eye size={16} className="mt-0.5 shrink-0" />
            <span>
              {reviewCount} transaction{reviewCount === 1 ? '' : 's'} to review after the recent sync —{' '}
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

      {transferCandidates.length > 0 && (
        <div className="mt-4 flex items-start justify-between gap-3 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm text-indigo-900">
          <div className="flex items-start gap-2">
            <ArrowRightLeft size={16} className="mt-0.5 shrink-0" />
            <span>
              {transferCandidates.length} possible transfer{transferCandidates.length === 1 ? '' : 's'} found between
              your own accounts. {transferCandidates.length === 1 ? "It isn't" : "They aren't"} counted in income or
              spending until you{' '}
              <button
                type="button"
                onClick={() => setReviewingTransfers(true)}
                className="underline hover:text-indigo-700"
              >
                review {transferCandidates.length === 1 ? 'it' : 'them'}
              </button>
              .
            </span>
          </div>
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => setNeedsReview((v) => !v)}
          aria-pressed={needsReview}
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
        {transferCandidates.length > 0 && (
          <button
            type="button"
            onClick={() => setReviewingTransfers(true)}
            className="inline-flex items-center gap-1.5 rounded-full border border-gray-300 bg-white px-3 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
          >
            <ArrowRightLeft size={12} />
            Possible transfers
            <span className="text-gray-500">· {transferCandidates.length}</span>
          </button>
        )}
        <button
          type="button"
          onClick={() => setMoreFiltersOpen((v) => !v)}
          aria-expanded={moreFiltersOpen}
          aria-controls="transaction-filters"
          className={[
            'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium md:hidden',
            moreFilterCount > 0
              ? 'border-indigo-300 bg-indigo-50 text-indigo-800'
              : 'border-gray-300 bg-white text-gray-700 hover:bg-gray-50',
          ].join(' ')}
        >
          <SlidersHorizontal size={12} />
          Filters
          {moreFilterCount > 0 && <span className="text-indigo-700">· {moreFilterCount}</span>}
        </button>
      </div>

      {moreFilterCount > 0 && !moreFiltersOpen && (
        <ul aria-label="Active filters" className="mt-2 flex flex-wrap gap-1.5 md:hidden">
          {activeFilterChips.map((c) => (
            <li key={c.key}>
              <button
                type="button"
                onClick={c.clear}
                aria-label={`Remove filter ${c.text}`}
                className="inline-flex min-h-8 max-w-full items-center gap-1 rounded-full border border-indigo-200 bg-indigo-50 px-2.5 text-xs font-medium text-indigo-800 hover:bg-indigo-100"
              >
                <span className="truncate">{c.text}</span>
                <X size={12} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div id="transaction-filters" className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-6">
        <Field label="Account" className={moreFilterClass}>
          <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
            <option value="">All</option>
            {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
        </Field>
        <Field label="Category" className={moreFilterClass}>
          <select className={inputClass} value={categoryID} onChange={(e) => setCategoryID(e.target.value)}>
            <option value="">All</option>
            <option value="null">Uncategorized</option>
            {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </Field>
        <Field label="From" className={moreFilterClass}>
          <input type="date" className={inputClass} value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        <Field label="To" className={moreFilterClass}>
          <input type="date" className={inputClass} value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
        <Field label="Tag" className={moreFilterClass}>
          <select className={inputClass} value={tag} onChange={(e) => setTag(e.target.value)}>
            <option value="">All</option>
            {/* Keep a tag picked from a row chip selectable even before the list loads. */}
            {tag !== '' && !tagOptions.some((o) => o.name === tag) && <option value={tag}>{tag}</option>}
            {tagOptions.map((o) => <option key={o.name} value={o.name}>{o.name} ({o.count})</option>)}
          </select>
        </Field>
        <Field label="Search">
          <input className={inputClass} placeholder="description or merchant" value={searchInput} onChange={(e) => setSearchInput(e.target.value)} />
        </Field>
      </div>

      {selectedIDs.length > 0 && (
        <div
          role="region"
          aria-label="Bulk actions"
          className="sticky top-0 z-10 mt-4 flex flex-wrap items-center gap-2 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-2 text-sm text-indigo-900"
        >
          <span className="font-medium">{selectedIDs.length} selected</span>
          <button
            type="button"
            disabled={reviewBusy}
            onClick={() => void reviewRows(selectedIDs, true)}
            className="inline-flex min-h-9 items-center gap-1.5 rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            <CheckCheck size={16} /> Mark reviewed
          </button>
          {!needsReview && (
            <button
              type="button"
              disabled={reviewBusy}
              onClick={() => void reviewRows(selectedIDs, false)}
              className="inline-flex min-h-9 items-center gap-1.5 rounded-md border border-indigo-300 bg-white px-3 py-1.5 text-sm font-medium text-indigo-800 hover:bg-indigo-100 disabled:opacity-50"
            >
              <Undo2 size={16} /> Needs review
            </button>
          )}
          <select
            aria-label="Set category for selected"
            disabled={reviewBusy}
            value=""
            onChange={(e) => setSelectedCategory(e.target.value, e.currentTarget)}
            className="min-h-9 max-w-[12rem] rounded-md border border-indigo-300 bg-white px-2 py-1.5 text-sm text-indigo-900 disabled:opacity-50"
          >
            <option value="">Set category…</option>
            <option value="null">Uncategorized</option>
            {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            <NewCategoryOption />
          </select>
          {anySelectedShown && (
            <button
              type="button"
              disabled={reviewBusy}
              onClick={() => void bulkEditRows(hideableSelectedIDs, { hidden: true }, (n) => `${plural(n)} hidden from spending and budgets.`)}
              className="inline-flex min-h-9 items-center gap-1.5 rounded-md border border-indigo-300 bg-white px-3 py-1.5 text-sm font-medium text-indigo-800 hover:bg-indigo-100 disabled:opacity-50"
            >
              <EyeOff size={16} /> Hide
            </button>
          )}
          {anySelectedHidden && (
            <button
              type="button"
              disabled={reviewBusy}
              onClick={() => void bulkEditRows(selectedIDs, { hidden: false }, (n) => `${plural(n)} back in spending and budgets.`)}
              className="inline-flex min-h-9 items-center gap-1.5 rounded-md border border-indigo-300 bg-white px-3 py-1.5 text-sm font-medium text-indigo-800 hover:bg-indigo-100 disabled:opacity-50"
            >
              <Eye size={16} /> Unhide
            </button>
          )}
          <button
            type="button"
            onClick={() => setSelected(new Set())}
            className="ml-auto min-h-9 px-2 text-sm text-indigo-700 underline"
          >
            Clear
          </button>
        </div>
      )}

      <div className="mt-4 overflow-x-auto rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="w-9 px-2 py-2 md:w-10 md:px-3">
                <input
                  type="checkbox"
                  aria-label="Select all transactions on this page"
                  checked={allSelected}
                  disabled={visibleIDs.length === 0}
                  onChange={() => setSelected(allSelected ? new Set() : new Set(visibleIDs))}
                  className="h-4 w-4"
                />
              </th>
              <th className="hidden px-3 py-2 text-left md:table-cell">Date</th>
              <th className="px-2 py-2 text-left md:px-3">
                <span className="flex justify-between gap-2">
                  Name
                  <span className="md:hidden">Amount</span>
                </span>
              </th>
              <th className="hidden px-3 py-2 text-left md:table-cell">Bank description</th>
              <th className="hidden px-3 py-2 text-right md:table-cell">Amount</th>
              {showAccount && <th className="hidden px-3 py-2 text-left md:table-cell">Account</th>}
              <th className="hidden px-3 py-2 text-left md:table-cell">Category</th>
              <th className="hidden px-3 py-2 md:table-cell"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {loading && transactions.length === 0 && (
              <tr><td colSpan={8} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && transactions.length === 0 && needsReview && reviewPending > 0 && (
              <tr><td colSpan={8} className="px-4 py-10 text-center text-gray-500" aria-live="polite">Saving your review…</td></tr>
            )}
            {!loading && transactions.length === 0 && needsReview && reviewPending === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-10 text-center">
                  <div className="text-base font-medium text-gray-900">You’re all caught up</div>
                  <div className="mt-1 text-sm text-gray-500">
                    Nothing left to review. New transactions from your next sync or import will show up here.
                  </div>
                  <button
                    type="button"
                    onClick={() => setNeedsReview(false)}
                    className="mt-3 min-h-9 rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
                  >
                    View all transactions
                  </button>
                </td>
              </tr>
            )}
            {!loading && transactions.length === 0 && !needsReview && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center">
                  {filtersActive ? (
                    <>
                      <div className="text-gray-700">No transactions match these filters.</div>
                      <button
                        type="button"
                        onClick={clearFilters}
                        className="mt-3 min-h-9 rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50"
                      >
                        Clear filters
                      </button>
                    </>
                  ) : (
                    <>
                      <div className="text-gray-700">No transactions yet.</div>
                      <div className="mt-1 text-sm text-gray-500">
                        Import a statement CSV, add one by hand, or connect a bank to get started.
                      </div>
                      <Link
                        to="/accounts/add"
                        className="mt-3 inline-flex min-h-9 items-center rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
                      >
                        {accounts.length === 0 ? 'Add your first account' : 'Add or import more'}
                      </Link>
                    </>
                  )}
                </td>
              </tr>
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
                    showAccount={showAccount}
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
                  showAccount={showAccount}
                  asset={assetByID.get(t.asset_id)}
                  categories={categories}
                  currentCategory={t.category_id ? categoryByID.get(t.category_id) : undefined}
                  selected={selected.has(t.id)}
                  onSelect={() => toggleSelected(t.id)}
                  onToggleReviewed={() => void reviewRows([t.id], t.reviewed_at == null)}
                  onCategoryChange={(catID) => { void setCategory(t.id, catID) }}
                  onNewCategory={(from) => setNewCategory({ apply: (c) => { void setCategory(t.id, c.id) }, from })}
                  onCreateRule={() => openRuleFromTxn(t)}
                  onSplit={() => setSplitting(t)}
                  onToggleHidden={() => { update(t.id, { hidden: t.hidden_at == null }).catch(() => {}) }}
                  onEditTags={() => setTagging(t)}
                  onEditDetails={() => setEditing(t)}
                  onTagClick={(name) => setTag(name)}
                  onDelete={async () => {
                    if (window.confirm('Delete this transaction?')) {
                      await remove(t.id)
                    }
                  }}
                  onUnlinkTransfer={async () => {
                    await unlinkTransfer(t.id)
                    refreshTransferCandidates()
                    await fetch()
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
            refreshTransferCandidates()
          }}
        />
      )}

      {reviewingTransfers && (
        <TransferMatchesModal
          candidates={transferCandidates}
          accountByID={accountByID}
          assetByID={assetByID}
          onClose={() => setReviewingTransfers(false)}
          onConfirmed={() => {
            refreshTransferCandidates()
            void fetch()
          }}
        />
      )}

      {importing && (
        <ImportTransactionsModal
          accounts={accounts}
          initialAccountID={filteredAccount?.id}
          onClose={() => setImporting(false)}
          onImported={() => { void fetch(); refreshTransferCandidates() }}
          onUndone={() => { void fetch(); refreshTransferCandidates() }}
        />
      )}

      {balancing && (
        <UpdateBalanceModal
          account={balancing}
          onClose={() => setBalancing(null)}
          onUpdated={() => { void fetchAccounts(); void fetch() }}
        />
      )}

      {splitting && (
        <SplitTransactionModal
          tx={splitting}
          currency={assetByID.get(splitting.asset_id)?.symbol ?? accountByID.get(splitting.account_id)?.currency ?? 'USD'}
          categories={categories}
          onClose={() => setSplitting(null)}
          onSaved={() => { void fetch() }}
        />
      )}

      {newCategory && (
        <NewCategoryDialog
          onCreated={(c) => { const { apply } = newCategory; setNewCategory(null); apply(c) }}
          onCancel={() => setNewCategory(null)}
          returnFocusTo={newCategory.from}
        />
      )}
      {tagging && (
        <TagEditorModal
          tx={tagging}
          suggestions={tagOptions}
          onClose={() => setTagging(null)}
          onSaved={() => { refreshTags(); void fetch() }}
        />
      )}

      {editing && (
        <EditDetailsModal
          tx={editing}
          currency={assetByID.get(editing.asset_id)?.symbol ?? accountByID.get(editing.account_id)?.currency ?? 'USD'}
          onClose={() => setEditing(null)}
          onSaved={() => { void fetch() }}
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

      {reviewToast && (
        <div
          role="status"
          className="fixed inset-x-4 bottom-4 z-30 flex items-center gap-3 rounded-md border border-gray-200 bg-white px-4 py-3 text-sm shadow-lg sm:left-auto sm:max-w-md"
        >
          <Check size={18} className="shrink-0 text-emerald-600" />
          <span className="flex-1 text-gray-900">{reviewToast.message}</span>
          <button
            type="button"
            onClick={() => {
              void reviewRows(reviewToast.ids, !reviewToast.reviewed, false)
            }}
            className="min-h-9 rounded-md px-2 text-sm font-medium text-indigo-700 hover:bg-indigo-50"
          >
            Undo
          </button>
          <button
            type="button"
            onClick={() => setReviewToast(null)}
            className="text-gray-400 hover:text-gray-700"
            aria-label="Dismiss"
          >×</button>
        </div>
      )}

      {bulkToast && (
        <div
          role="status"
          className="fixed inset-x-4 bottom-4 z-30 flex items-center gap-3 rounded-md border border-gray-200 bg-white px-4 py-3 text-sm shadow-lg sm:left-auto sm:max-w-md"
        >
          <Check size={18} className="shrink-0 text-emerald-600" />
          <span className="flex-1 text-gray-900">{bulkToast.message}</span>
          {bulkToast.undo && (
            <button
              type="button"
              disabled={reviewBusy}
              onClick={() => { if (bulkToast.undo) void undoBulkRows(bulkToast.undo) }}
              className="min-h-9 rounded-md px-2 text-sm font-medium text-indigo-700 hover:bg-indigo-50 disabled:opacity-50"
            >
              Undo
            </button>
          )}
          <button
            type="button"
            onClick={() => setBulkToast(null)}
            className="text-gray-400 hover:text-gray-700"
            aria-label="Dismiss"
          >×</button>
        </div>
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
  // false when the list is filtered to this one account (#675).
  showAccount?: boolean
  asset?: Asset
  categories: Category[]
  currentCategory?: Category
  onCategoryChange: (catID: number | null) => void
  // Offers "+ New category…" in the row picker (#581) when set.
  onNewCategory?: (from: HTMLElement) => void
  onCreateRule: () => void
  // Opens the split modal (#438); absent for rows that can't be split.
  onSplit?: () => void
  // Hide/unhide from spending and budgets (#465); absent for non-flow rows.
  onToggleHidden?: () => void
  // Tags (#544): open the editor / filter the list by a tag chip.
  onEditTags?: () => void
  // Opens the merchant/notes editor (#569).
  onEditDetails?: () => void
  onTagClick?: (tag: string) => void
  onDelete: () => Promise<void>
  // Review state (#424). Absent for rows that don't take part in review
  // (legs of an expanded paired trade).
  selected?: boolean
  onSelect?: () => void
  onToggleReviewed?: () => void
  // Unlinks a confirmed transfer match (#371). Absent for rows that can
  // never be a transfer leg (e.g. paired-trade legs rendered read-only).
  onUnlinkTransfer?: () => Promise<void>
  // Optional indent for rows rendered as legs of an expanded paired
  // trade. Keeps the visual hierarchy clear.
  indented?: boolean
}

const PENDING_HINT = 'Your bank hasn’t posted this yet. Your edits will carry over when it posts.'

function TransactionRow({ tx, account, showAccount = true, asset, categories, currentCategory, onCategoryChange, onNewCategory, onCreateRule, onSplit, onToggleHidden, onEditTags, onEditDetails, onTagClick, onDelete, onUnlinkTransfer, indented, selected, onSelect, onToggleReviewed }: RowProps) {
  // Per ADR-0013, the unit on a transaction is asset_id. Display currency
  // = asset.symbol when available; fall back to the parent account's
  // currency for older rows that haven't been backfilled.
  const currency = asset?.symbol ?? account?.currency ?? 'USD'
  // opening_balance/adjustment rows are reconciliation facts (ADR-0017), not
  // ordinary flow — flag them visually and link to the per-account
  // reconciliation view (#370) rather than letting them blend into spending.
  const isReconciling = tx.kind === 'opening_balance' || tx.kind === 'adjustment'
  const reviewable = onToggleReviewed != null && tx.kind === 'flow'
  const unreviewed = reviewable && tx.reviewed_at == null
  const label = tx.description ?? tx.merchant_name ?? 'transaction'
  // Rows lead with the name the user recognises (#579); the raw bank string
  // stays in its own column (desktop) or the meta line (phone).
  const displayName = txDisplayName(tx) || '—'
  const rawDescription = txSecondaryDescription(tx)
  const splitCount = tx.splits?.length ?? 0
  const tags = tx.tags ?? []
  // A row already out of spending as a transfer gets no "Hide from
  // spending" (#643); one hidden before it paired can still be unhidden.
  const hideable = onToggleHidden != null && tx.kind === 'flow' &&
    (tx.hidden_at != null || !excludedAsTransfer(tx, currentCategory))
  const hidden = hideable && tx.hidden_at != null
  const splittable = onSplit != null && tx.kind === 'flow' && !tx.is_transfer
  const stale = splitCount > 0 && tx.splits_need_attention === true
  // An unpaired Transfer-category row is excluded from spending/income/budgets
  // (#461) — say so, otherwise the list and Insights look inconsistent.
  const unpairedTransfer = tx.kind === 'flow' && !tx.is_transfer && splitCount === 0 && currentCategory?.slug === 'transfer'
  const categorySelect = splitCount > 0 ? (
    <button
      type="button"
      onClick={onSplit}
      aria-label={stale ? `Re-split ${label}: amount changed` : `Edit split for ${label}`}
      className={stale
        ? 'min-h-9 rounded border border-amber-300 bg-amber-50 px-2 py-1 text-sm text-amber-900 hover:bg-amber-100'
        : 'min-h-9 rounded border border-indigo-200 bg-indigo-50 px-2 py-1 text-sm text-indigo-800 hover:bg-indigo-100'}
    >
      {stale ? 'Amount changed · re-split' : `Split · ${splitCount} categories`}
    </button>
  ) : (
    <select
      aria-label={`Category for ${label}`}
      value={currentCategory ? currentCategory.id : ''}
      onChange={(e) => {
        const v = e.target.value
        if (v === NEW_CATEGORY) {
          // The select stays controlled, so it snaps back to the current
          // category; the dialog selects the new one once it exists.
          onNewCategory?.(e.currentTarget)
          return
        }
        onCategoryChange(v === '' ? null : Number(v))
      }}
      // truncate: a long name ends in an ellipsis before the chevron instead
      // of being cut mid-letter on a phone (#579).
      className="min-h-9 w-full min-w-0 truncate rounded border border-gray-300 px-1.5 py-1 text-sm md:w-auto"
      // A name longer than the box still reads in full on hover / long-press.
      title={currentCategory?.name ?? 'Uncategorized'}
    >
      <option value="">Uncategorized</option>
      {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
      {onNewCategory && <NewCategoryOption />}
    </select>
  )
  const note = tx.notes?.trim() ?? ''
  const rowActions: RowAction[] = [
    ...(onEditDetails ? [{ key: 'details', ariaLabel: `Edit details for ${label}`, label: <><Pencil size={16} /> Edit details</>, onSelect: onEditDetails }] : []),
    { key: 'rule', ariaLabel: `Create rule from ${label}`, label: <><Sparkles size={16} /> Create rule</>, onSelect: onCreateRule },
    ...(splittable && splitCount === 0 && onSplit
      ? [{ key: 'split', ariaLabel: `Split ${label}`, label: <><Scissors size={16} /> Split</>, onSelect: onSplit }]
      : []),
    ...(onEditTags ? [{ key: 'tags', ariaLabel: `Tags for ${label}`, label: <><Tag size={16} /> Tags</>, onSelect: onEditTags }] : []),
    ...(hideable && onToggleHidden
      ? [{
          key: 'hide',
          ariaLabel: hidden ? `Unhide ${label}` : `Hide ${label}`,
          label: hidden ? <><Eye size={16} /> Unhide</> : <><EyeOff size={16} /> Hide from spending</>,
          onSelect: onToggleHidden,
        }]
      : []),
    { key: 'delete', ariaLabel: `Delete ${label}`, label: <><Trash2 size={16} /> Delete</>, onSelect: () => { void onDelete() }, danger: true },
  ]
  return (
    <tr className={`hover:bg-gray-50 ${isReconciling ? 'bg-amber-50/60' : ''} ${unreviewed ? 'border-l-2 border-l-amber-400' : ''} ${hidden ? 'text-gray-500 opacity-60' : ''}`}>
      <td className="px-2 py-2 align-top md:px-3">
        {reviewable && onSelect && (
          <input
            type="checkbox"
            aria-label={`Select ${label}`}
            checked={!!selected}
            onChange={onSelect}
            className="mt-1.5 h-4 w-4"
          />
        )}
      </td>
      <td className={`hidden whitespace-nowrap px-3 py-2 align-top text-gray-700 md:table-cell ${indented ? 'pl-8' : ''}`}><DateDisplay value={tx.transaction_date} /></td>
      <td className="px-2 py-2 align-top text-gray-900 md:px-3">
        {/* A phone has no Amount column: the amount sits on the name line so
            the category picker below gets the row's full width (#590). */}
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <span className="font-medium">{displayName}</span>
            {unreviewed && <span className="sr-only"> (needs review)</span>}
            {isReconciling && account && (
              <Link
                to={`/accounts/${account.id}/reconciliation`}
                title="Reconciliation row — see the account's reconciliation view."
                className="ml-1.5 inline-flex items-center rounded border border-amber-200 bg-amber-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-amber-700 hover:bg-amber-100"
              >
                {tx.kind === 'opening_balance' ? 'opening balance' : 'adjustment'}
              </Link>
            )}
            {tx.is_transfer && onUnlinkTransfer && (
              <button
                type="button"
                onClick={() => {
                  if (window.confirm('Unlink this transfer? Both sides will go back to counting as ordinary transactions.')) {
                    void onUnlinkTransfer()
                  }
                }}
                title="Confirmed transfer — click to unlink"
                className="ml-1.5 inline-flex items-center rounded border border-indigo-200 bg-indigo-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-indigo-700 hover:bg-indigo-100"
              >
                transfer
              </button>
            )}
            {tx.pending && (
              <span
                title={PENDING_HINT}
                className="ml-1.5 inline-flex items-center rounded border border-sky-200 bg-sky-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-sky-700"
              >
                pending
              </span>
            )}
            {hidden && (
              <span
                title="You hid this transaction. It still counts toward the account balance but not spending, income or budgets."
                className="ml-1.5 inline-flex items-center rounded border border-gray-200 bg-gray-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-gray-600"
              >
                hidden
              </span>
            )}
            {note !== '' && (
              <button
                type="button"
                onClick={onEditDetails}
                title={note}
                aria-label={`Note: ${note}. Edit details for ${label}`}
                className="ml-1.5 hidden items-center md:inline-flex rounded border border-amber-200 bg-amber-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-amber-800 hover:bg-amber-100"
              >
                note
              </button>
            )}
            {unpairedTransfer && (
              <span
                title="This row is in the Transfer category but has no matching leg, so it is not counted in spending, income or budgets."
                className="ml-1.5 inline-flex items-center rounded border border-gray-200 bg-gray-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-gray-600"
              >
                not in spending
              </span>
            )}
            {tags.length > 0 && (
              <div className="mt-1 flex flex-wrap gap-1">
                {tags.map((name) => (
                  <button
                    key={name}
                    type="button"
                    onClick={() => onTagClick?.(name)}
                    aria-label={`Show transactions tagged ${name}`}
                    title={`Show transactions tagged ${name}`}
                    className="inline-flex max-w-full items-center gap-1 truncate rounded-full border border-violet-200 bg-violet-50 px-2 py-px text-[11px] font-medium leading-4 text-violet-800 hover:bg-violet-100"
                  >
                    <Tag size={10} aria-hidden="true" />
                    {name}
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="shrink-0 whitespace-nowrap text-right md:hidden">
            <AmountDisplay amount={tx.amount} currency={currency} signed />
          </div>
        </div>
        {/* Narrow screens drop the Merchant/Account/Category columns; show
            them stacked under the description instead of a squeezed table. */}
        <div className="mt-1 space-y-1.5 md:hidden">
          <div className="text-xs text-gray-500">
            <DateDisplay value={tx.transaction_date} />
            {rawDescription && (
              <> · <span className="break-all" title={rawDescription}>{rawDescription}</span></>
            )}
            {showAccount && account?.name && ` · ${account.name}`}
          </div>
          {/* No hover on touch screens: spell out what the chip's tooltip says. */}
          {tx.pending && <div className="text-xs text-sky-700">{PENDING_HINT}</div>}
          {/* No hover on a phone: the note is written out (one line) and
              tapping it opens Edit details, instead of a chip (#569). */}
          {note !== '' && (
            <button
              type="button"
              onClick={onEditDetails}
              title={note}
              className="block w-full truncate text-left text-xs italic text-gray-600"
            >
              {note}
            </button>
          )}
          {/* Daily review stays inline: category, reviewed, then one menu
              for the rest, so a row stays short on a phone (#554). */}
          <div className="flex items-center gap-1.5">
            <div className="min-w-0 flex-1">{categorySelect}</div>
            {reviewable && (
              <button
                type="button"
                onClick={onToggleReviewed}
                aria-label={unreviewed ? `Mark ${label} reviewed` : `Reviewed: ${label} — tap to move back to review`}
                title={unreviewed ? 'Mark reviewed' : 'Reviewed — tap to move back to review'}
                className={`inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md border ${
                  unreviewed
                    ? 'border-emerald-300 bg-emerald-50 text-emerald-700'
                    : 'border-transparent text-emerald-600'
                }`}
              >
                <Check size={18} />
              </button>
            )}
            <RowActionsMenu label={label} actions={rowActions} />
          </div>
        </div>
      </td>
      <td className="hidden max-w-56 px-3 py-2 align-top text-xs text-gray-500 md:table-cell">
        {/* Empty when the name already is the bank string — no repeat. */}
        {rawDescription && <div className="truncate" title={rawDescription}>{rawDescription}</div>}
      </td>
      <td className="hidden whitespace-nowrap px-3 py-2 text-right align-top md:table-cell">
        <AmountDisplay amount={tx.amount} currency={currency} signed />
      </td>
      {showAccount && <td className="hidden px-3 py-2 align-top text-gray-700 md:table-cell">{account?.name ?? `#${tx.account_id}`}</td>}
      <td className="hidden px-3 py-2 align-top md:table-cell">{categorySelect}</td>
      <td className="hidden px-3 py-2 align-top md:table-cell">
        {/* Desktop mirrors the phone (#554): Reviewed stays inline and the
            rest share one "More actions" menu, so a row is one line of
            controls instead of six icons wrapping to two rows (#563). */}
        <div className="flex items-center justify-end gap-1">
          {reviewable && (
            <button
              type="button"
              onClick={onToggleReviewed}
              className={`inline-flex min-h-8 min-w-8 items-center justify-center rounded-md ${
                unreviewed
                  ? 'border border-emerald-300 bg-emerald-50 text-emerald-700 hover:bg-emerald-100'
                  : 'text-emerald-600 hover:bg-gray-100'
              }`}
              aria-label={unreviewed ? `Mark ${label} reviewed` : `Reviewed: ${label} — click to move back to review`}
              title={unreviewed ? 'Mark reviewed' : 'Reviewed — click to undo'}
            >
              <Check size={16} />
            </button>
          )}
          <RowActionsMenu label={label} actions={rowActions} compact />
        </div>
      </td>
    </tr>
  )
}

type PairedTradeRowProps = {
  legA: Transaction
  legB: Transaction
  account?: Account
  showAccount?: boolean
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
  showAccount = true,
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
        <td className="px-2 py-2 md:px-3"></td>
        <td className="hidden px-3 py-2 text-gray-700 md:table-cell">
          <DateDisplay value={securityLeg.transaction_date} />
        </td>
        <td className="px-2 py-2 text-gray-900 md:px-3">
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
          <div className="mt-1 text-xs text-gray-500 md:hidden">
            <DateDisplay value={securityLeg.transaction_date} />
            {showAccount && ` · ${account?.name ?? `#${securityLeg.account_id}`}`}
          </div>
          {/* No Amount column on a phone (#590): both legs under the name. */}
          <div className="mt-1 flex gap-2 text-xs tabular-nums md:hidden">
            <AmountDisplay amount={securityLeg.amount} currency={securityCurrency} signed />
            <span className="text-gray-500">
              <AmountDisplay amount={cashLeg.amount} currency={cashCurrency} signed />
            </span>
          </div>
        </td>
        <td className="hidden px-3 py-2 text-gray-400 md:table-cell">—</td>
        <td className="hidden px-3 py-2 text-right md:table-cell">
          <div className="tabular-nums">
            <AmountDisplay amount={securityLeg.amount} currency={securityCurrency} signed />
          </div>
          <div className="tabular-nums text-xs text-gray-500">
            <AmountDisplay amount={cashLeg.amount} currency={cashCurrency} signed />
          </div>
        </td>
        {showAccount && <td className="hidden px-3 py-2 text-gray-700 md:table-cell">{account?.name ?? `#${securityLeg.account_id}`}</td>}
        <td className="hidden px-3 py-2 text-gray-400 italic text-xs md:table-cell">expand to edit</td>
        <td className="hidden px-3 py-2 md:table-cell"></td>
      </tr>
      {expanded && (
        <>
          <TransactionRow
            tx={securityLeg}
            account={account}
            showAccount={showAccount}
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
            showAccount={showAccount}
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
    <ModalShell aria-label="Add transaction">
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
    </ModalShell>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

// AccountHeader sits over a list filtered to one account (#657): the
// account's balance as Accounts shows it, and the way into its
// reconciliation and (manual cash accounts) a fresh balance.
function AccountHeader({ account, onUpdateBalance, onShowAll }: {
  account: Account
  onUpdateBalance: () => void
  onShowAll: () => void
}) {
  return (
    <section
      aria-label={`Account ${account.name}`}
      className="mt-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-lg border border-gray-200 bg-white px-4 py-3"
    >
      <div className="min-w-0">
        <h2 className="break-words text-base font-semibold text-gray-900">{account.name}</h2>
        <div className="text-xs text-gray-500">{accountTypeLabel(account.account_type)}</div>
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
        <div className="text-right">
          <div className="text-xs font-medium uppercase tracking-wider text-gray-500">Balance</div>
          <div aria-label="Account balance" className="text-lg font-semibold text-gray-900">
            <AccountBalance accountType={account.account_type} balance={account.balance} currency={account.currency} />
            {!account.balance_complete && <PartialBadge />}
          </div>
        </div>
        <nav aria-label={`${account.name} actions`} className="flex flex-wrap items-center gap-x-3 text-sm">
          <Link
            to={`/accounts/${account.id}/reconciliation`}
            className="inline-flex min-h-10 items-center font-medium text-indigo-600 hover:text-indigo-800"
          >
            Reconciliation
          </Link>
          {canUpdateBalance(account) && (
            <button
              type="button"
              onClick={onUpdateBalance}
              className="inline-flex min-h-10 items-center font-medium text-indigo-600 hover:text-indigo-800"
            >
              Update balance
            </button>
          )}
          <button
            type="button"
            onClick={onShowAll}
            className="inline-flex min-h-10 items-center text-gray-600 hover:text-gray-900"
          >
            All accounts
          </button>
        </nav>
      </div>
    </section>
  )
}

function Field({ label, className = 'block', children }: { label: string; className?: string; children: ReactNode }) {
  return (
    <label className={`${className} text-sm`}>
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
  )
}

// excludedAsTransfer: a paired transfer leg, or an unsplit Transfer-category
// row (#461), is already left out of spending, income and budgets.
function excludedAsTransfer(tx: Transaction, category: Category | undefined): boolean {
  return tx.is_transfer || ((tx.splits?.length ?? 0) === 0 && category?.slug === 'transfer')
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
