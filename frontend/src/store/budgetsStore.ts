import { create } from 'zustand'
import {
  createBudget as apiCreate,
  deleteBudget as apiDelete,
  getBudgetSpend,
  listBudgets,
  updateBudget as apiUpdate,
} from '../api/budgets'
import type {
  Budget,
  BudgetSpend,
  CreateBudgetInput,
  UpdateBudgetInput,
} from '../types/budget'

type State = {
  budgets: Budget[]
  // spendByBudgetID is fetched lazily after the budgets list lands. Each
  // entry is the result of /budgets/:id/spend at fetch time. We don't
  // refresh on focus — the dashboard is the live-spend surface.
  spendByBudgetID: Record<number, BudgetSpend>
  loading: boolean
  error: string | null
  code: string | null
  fetch: () => Promise<void>
  create: (input: CreateBudgetInput) => Promise<Budget>
  update: (id: number, input: UpdateBudgetInput) => Promise<Budget>
  remove: (id: number) => Promise<void>
  clearError: () => void
}

export const useBudgetsStore = create<State>((set, get) => ({
  budgets: [],
  spendByBudgetID: {},
  loading: false,
  error: null,
  code: null,

  fetch: async () => {
    set({ loading: true, error: null, code: null })
    try {
      const items = await listBudgets()
      set({ budgets: items, loading: false })
      // Fan out per-budget spend fetches in parallel; ignore individual
      // failures so a transient error on one budget doesn't blank the
      // whole page.
      const spend: Record<number, BudgetSpend> = {}
      await Promise.allSettled(
        items.filter((b) => b.is_active).map(async (b) => {
          try {
            spend[b.id] = await getBudgetSpend(b.id)
          } catch {
            /* leave undefined; page shows "—" */
          }
        }),
      )
      set({ spendByBudgetID: spend })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  create: async (input) => {
    set({ error: null, code: null })
    try {
      const b = await apiCreate(input)
      await get().fetch()
      return b
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  update: async (id, input) => {
    set({ error: null, code: null })
    try {
      const b = await apiUpdate(id, input)
      // Refetch to refresh both the row and its spend in one place.
      await get().fetch()
      return b
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  remove: async (id) => {
    set({ error: null, code: null })
    try {
      await apiDelete(id)
      set({
        budgets: get().budgets.filter((b) => b.id !== id),
        spendByBudgetID: Object.fromEntries(
          Object.entries(get().spendByBudgetID).filter(([k]) => Number(k) !== id),
        ),
      })
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  clearError: () => set({ error: null, code: null }),
}))

function errInfo(err: unknown): { error: string; code: string | null } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { error: r.data.error, code: r.data.code ?? null }
  }
  if (err instanceof Error) return { error: err.message, code: null }
  return { error: 'request failed', code: null }
}
