import { create } from 'zustand'
import {
  contributeToGoal as apiContribute,
  createGoal as apiCreate,
  deleteGoal as apiDelete,
  listGoals,
  updateGoal as apiUpdate,
} from '../api/savingsGoals'
import type {
  ContributionInput,
  CreateGoalInput,
  SavingsGoal,
  UpdateGoalInput,
} from '../types/savingsGoal'

type State = {
  goals: SavingsGoal[]
  loading: boolean
  error: string | null
  code: string | null
  fetch: () => Promise<void>
  create: (input: CreateGoalInput) => Promise<SavingsGoal>
  update: (id: number, input: UpdateGoalInput) => Promise<SavingsGoal>
  remove: (id: number) => Promise<void>
  contribute: (id: number, input: ContributionInput) => Promise<SavingsGoal>
  clearError: () => void
}

export const useSavingsGoalsStore = create<State>((set, get) => ({
  goals: [],
  loading: false,
  error: null,
  code: null,

  fetch: async () => {
    set({ loading: true, error: null, code: null })
    try {
      const items = await listGoals()
      set({ goals: items, loading: false })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  create: async (input) => {
    set({ error: null, code: null })
    try {
      const g = await apiCreate(input)
      await get().fetch()
      return g
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  update: async (id, input) => {
    set({ error: null, code: null })
    try {
      const g = await apiUpdate(id, input)
      set({ goals: get().goals.map((x) => (x.id === id ? g : x)) })
      return g
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  remove: async (id) => {
    set({ error: null, code: null })
    try {
      await apiDelete(id)
      set({ goals: get().goals.filter((x) => x.id !== id) })
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  contribute: async (id, input) => {
    set({ error: null, code: null })
    try {
      const g = await apiContribute(id, input)
      set({ goals: get().goals.map((x) => (x.id === id ? g : x)) })
      return g
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
