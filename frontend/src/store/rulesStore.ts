import { create } from 'zustand'
import {
  applyRules as apiApply,
  createRule as apiCreate,
  deleteRule as apiDelete,
  listRules,
  updateRule as apiUpdate,
} from '../api/categorizationRules'
import type {
  ApplyResult,
  ApplyScope,
  CategorizationRule,
  CreateRuleInput,
  UpdateRuleInput,
} from '../types/categorizationRule'

type State = {
  rules: CategorizationRule[]
  loading: boolean
  // error is the human-readable message; code is the machine-readable hint
  // (INVALID_REGEX, UNKNOWN_CATEGORY, ...). Forms switch on code to attach
  // the error to a specific field — see RulesPage.
  error: string | null
  code: string | null
  fetch: () => Promise<void>
  create: (input: CreateRuleInput) => Promise<CategorizationRule>
  update: (id: number, input: UpdateRuleInput) => Promise<CategorizationRule>
  remove: (id: number) => Promise<void>
  apply: (scope?: ApplyScope) => Promise<ApplyResult>
  clearError: () => void
}

export const useRulesStore = create<State>((set, get) => ({
  rules: [],
  loading: false,
  error: null,
  code: null,

  fetch: async () => {
    set({ loading: true, error: null, code: null })
    try {
      const items = await listRules()
      set({ rules: items, loading: false })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  create: async (input) => {
    set({ error: null, code: null })
    try {
      const rule = await apiCreate(input)
      await get().fetch()
      return rule
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  update: async (id, input) => {
    set({ error: null, code: null })
    try {
      const rule = await apiUpdate(id, input)
      set({ rules: get().rules.map((r) => (r.id === id ? rule : r)) })
      return rule
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  remove: async (id) => {
    set({ error: null, code: null })
    try {
      await apiDelete(id)
      set({ rules: get().rules.filter((r) => r.id !== id) })
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  apply: async (scope) => {
    set({ error: null, code: null })
    try {
      const result = await apiApply(scope)
      // Re-fetch transactions is a page-level concern; rules themselves
      // didn't change, so no rules-store refresh needed.
      return result
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
