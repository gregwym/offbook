import { create } from 'zustand'
import {
  bulkEditTransactions,
  createTransaction as apiCreate,
  deleteTransaction as apiDelete,
  listTransactions,
  setTransactionsReviewed,
  undoBulkEdit as apiUndoBulkEdit,
  updateTransaction as apiUpdate,
  type TransactionListFilter,
} from '../api/transactions'
import type {
  BulkEditAction,
  BulkEditPrior,
  BulkEditResult,
  CreateTransactionInput,
  Transaction,
  UpdateTransactionInput,
} from '../types/transaction'

type State = {
  transactions: Transaction[]
  total: number
  // The spending figure behind a spending-lens list (#571); null otherwise.
  spendTotal: string | null
  loading: boolean
  error: string | null
  filter: TransactionListFilter
  // Pagination: 0-based page; computed offset = page * pageSize.
  page: number
  pageSize: number
  // Review writes still in flight (#633). The inbox must not claim "all
  // caught up" while the server has not yet saved that state.
  reviewPending: number

  fetch: () => Promise<void>
  setFilter: (f: TransactionListFilter) => void
  setPage: (p: number) => void
  create: (input: CreateTransactionInput) => Promise<Transaction>
  // Optimistic update: applies locally, then PATCHes; reverts on failure.
  setCategory: (id: number, categoryID: number | null) => Promise<void>
  update: (id: number, input: UpdateTransactionInput) => Promise<Transaction>
  remove: (id: number) => Promise<void>
  // Optimistic bulk review/undo (#424). In the review inbox (filter.reviewed
  // === false) reviewed rows leave the list immediately; reverts on failure.
  // Writes are sent one at a time in click order, so the last action is the
  // one the server keeps (#633).
  markReviewed: (ids: number[], reviewed: boolean) => Promise<void>
  // Bulk category / hide edit and its undo (#567). Both reload the page
  // afterwards, since the edit can move rows in or out of the active filter.
  // Errors land in `error` and are rethrown.
  bulkEdit: (ids: number[], action: BulkEditAction) => Promise<BulkEditResult>
  undoBulkEdit: (undo: BulkEditPrior[]) => Promise<{ restored: number; kept: number }>
}

const PAGE_SIZE = 50

// Monotonic token so a slow, superseded response (e.g. the mount fetch with the
// default filter landing after the URL-seeded ?review=1 fetch) cannot overwrite
// the list the active filter asked for (#458).
let fetchSeq = 0

// Tail of the review-write chain (#633). Each markReviewed POST waits for the
// previous one to settle, so an Undo and a quick re-review can't reach the
// server out of order.
let reviewChain: Promise<unknown> = Promise.resolve()

export const useTransactionsStore = create<State>((set, get) => ({
  transactions: [],
  total: 0,
  spendTotal: null,
  loading: false,
  error: null,
  filter: {},
  page: 0,
  pageSize: PAGE_SIZE,
  reviewPending: 0,

  fetch: async () => {
    const { filter, page, pageSize } = get()
    const seq = ++fetchSeq
    set({ loading: true, error: null })
    try {
      const { items, total, spend_total } = await listTransactions({
        ...filter,
        limit: pageSize,
        offset: page * pageSize,
      })
      if (seq !== fetchSeq) return
      // A write elsewhere (review, bulk edit, delete) can shrink the list
      // so the page we asked for no longer exists (#708). Step back to the
      // last real page instead of showing an empty page with no way back.
      const last = lastPage(total, pageSize)
      if (items.length === 0 && page > last) {
        set({ page: last })
        return get().fetch()
      }
      set({ transactions: items, total, spendTotal: spend_total ?? null, loading: false })
    } catch (err) {
      if (seq !== fetchSeq) return
      set({ loading: false, error: errMsg(err) })
    }
  },

  setFilter: (f) => {
    // Reset to first page whenever filters change — pagination of stale results
    // produces ghost rows.
    set({ filter: f, page: 0 })
    void get().fetch()
  },

  setPage: (p) => {
    set({ page: Math.max(0, p) })
    void get().fetch()
  },

  create: async (input) => {
    set({ error: null })
    try {
      const t = await apiCreate(input)
      // Refetch (server-side filter + ordering wins).
      await get().fetch()
      return t
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  setCategory: async (id, categoryID) => {
    const before = get().transactions
    const filterMethod = get().filter.categorization_method
    // Picking a category server-side flips categorization_method to
    // 'manual' (clearing it nulls the method out). Mirror that locally
    // so a row immediately leaves the "Needs review" view per the v6 §04
    // acceptance criterion — no full reload.
    const newMethod: string | null = categoryID === null ? null : 'manual'
    const next = before
      .map((t) =>
        t.id === id
          ? { ...t, category_id: categoryID, categorization_method: newMethod ?? undefined }
          : t,
      )
      // If the active filter restricts to a different method (e.g.
      // 'plaid_default'), the updated row no longer belongs in this list.
      // Filtering happens after the map so the user sees the change before
      // the row vanishes.
      .filter((t) => (filterMethod ? t.categorization_method === filterMethod : true))
    set({ transactions: next })
    try {
      if (categoryID === null) {
        await apiUpdate(id, { clear_category: true })
      } else {
        await apiUpdate(id, { category_id: categoryID })
      }
    } catch (err) {
      // Revert.
      set({ transactions: before, error: errMsg(err) })
      throw err
    }
  },

  update: async (id, input) => {
    set({ error: null })
    try {
      const t = await apiUpdate(id, input)
      set({ transactions: get().transactions.map((x) => (x.id === id ? t : x)) })
      return t
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  bulkEdit: async (ids, action) => {
    set({ error: null })
    try {
      const res = await bulkEditTransactions(ids, action)
      await get().fetch()
      return res
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  undoBulkEdit: async (undo) => {
    set({ error: null })
    try {
      const res = await apiUndoBulkEdit(undo)
      await get().fetch()
      return res
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  markReviewed: async (ids, reviewed) => {
    const { transactions: before, total: totalBefore, filter } = get()
    const idSet = new Set(ids)
    const stamp = new Date().toISOString()
    let next = before.map((t) =>
      idSet.has(t.id) ? { ...t, reviewed_at: reviewed ? (t.reviewed_at ?? stamp) : null } : t,
    )
    let total = totalBefore
    if (filter.reviewed !== undefined) {
      const leaving = next.filter((t) => idSet.has(t.id) && (t.reviewed_at != null) !== filter.reviewed)
      next = next.filter((t) => !leaving.includes(t))
      total = Math.max(0, totalBefore - leaving.length)
    }
    set({ transactions: next, total, error: null, reviewPending: get().reviewPending + 1 })
    const write = reviewChain.then(() => setTransactionsReviewed(ids, reviewed))
    reviewChain = write.catch(() => undefined)
    let ok = false
    try {
      await write
      ok = true
    } catch (err) {
      set({ transactions: before, total: totalBefore, error: errMsg(err) })
      throw err
    } finally {
      const pending = get().reviewPending - 1
      const s = get()
      // The rows on screen all left the filter but more still match it
      // (e.g. the sole row on page 2 of 51, #708): reload a real page
      // rather than announce an empty inbox. Only once the last queued
      // write has landed, so the reload sees every saved review.
      if (ok && pending === 0 && s.transactions.length === 0 && s.total > 0) {
        set({ reviewPending: 0, page: Math.min(s.page, lastPage(s.total, s.pageSize)), loading: true })
        void get().fetch()
      } else {
        set({ reviewPending: pending })
      }
    }
  },

  remove: async (id) => {
    set({ error: null })
    try {
      await apiDelete(id)
      const { transactions, total } = get()
      const next = transactions.filter((t) => t.id !== id)
      set({ transactions: next, total: Math.max(0, total - (transactions.length - next.length)) })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },
}))

// Highest valid 0-based page for `total` rows (0 when the list is empty).
function lastPage(total: number, pageSize: number): number {
  return Math.max(0, Math.ceil(total / pageSize) - 1)
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
