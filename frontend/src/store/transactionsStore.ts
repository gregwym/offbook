import { create } from 'zustand'
import {
  createTransaction as apiCreate,
  deleteTransaction as apiDelete,
  listTransactions,
  updateTransaction as apiUpdate,
  type TransactionListFilter,
} from '../api/transactions'
import type {
  CreateTransactionInput,
  Transaction,
  UpdateTransactionInput,
} from '../types/transaction'

type State = {
  transactions: Transaction[]
  total: number
  loading: boolean
  error: string | null
  filter: TransactionListFilter
  // Pagination: 0-based page; computed offset = page * pageSize.
  page: number
  pageSize: number

  fetch: () => Promise<void>
  setFilter: (f: TransactionListFilter) => void
  setPage: (p: number) => void
  create: (input: CreateTransactionInput) => Promise<Transaction>
  // Optimistic update: applies locally, then PATCHes; reverts on failure.
  setCategory: (id: number, categoryID: number | null) => Promise<void>
  update: (id: number, input: UpdateTransactionInput) => Promise<Transaction>
  remove: (id: number) => Promise<void>
}

const PAGE_SIZE = 50

export const useTransactionsStore = create<State>((set, get) => ({
  transactions: [],
  total: 0,
  loading: false,
  error: null,
  filter: {},
  page: 0,
  pageSize: PAGE_SIZE,

  fetch: async () => {
    const { filter, page, pageSize } = get()
    set({ loading: true, error: null })
    try {
      const { items, total } = await listTransactions({
        ...filter,
        limit: pageSize,
        offset: page * pageSize,
      })
      set({ transactions: items, total, loading: false })
    } catch (err) {
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
    // Optimistic update.
    set({
      transactions: before.map((t) => (t.id === id ? { ...t, category_id: categoryID } : t)),
    })
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

  remove: async (id) => {
    set({ error: null })
    try {
      await apiDelete(id)
      set({ transactions: get().transactions.filter((t) => t.id !== id) })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },
}))

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
