import { create } from 'zustand'
import { getScope, setScope as apiSetScope } from '../api/scope'
import { SCOPE_PERSONAL, type Scope } from '../types/scope'

type ScopeState = {
  active: Scope
  available: Scope[]
  householdId: number | null
  hydrated: boolean
  loading: boolean
  error: string | null
  hydrate: () => Promise<void>
  setScope: (scope: Scope) => Promise<void>
}

// scopeStore is the canonical client-side reflection of /me/scope. The
// AppShell sidebar reads `active` + `available` to decide which route list
// to render. Mutations go through PATCH /me/scope so the server is the
// source of truth — we never mutate `active` locally without persisting.
export const useScopeStore = create<ScopeState>((set) => ({
  active: SCOPE_PERSONAL,
  available: [SCOPE_PERSONAL],
  householdId: null,
  hydrated: false,
  loading: false,
  error: null,

  hydrate: async () => {
    set({ loading: true, error: null })
    try {
      const view = await getScope()
      set({
        active: view.active,
        available: view.available,
        householdId: view.household_id ?? null,
        hydrated: true,
        loading: false,
      })
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'failed to load scope',
        loading: false,
      })
    }
  },

  setScope: async (scope) => {
    set({ loading: true, error: null })
    try {
      const view = await apiSetScope(scope)
      set({
        active: view.active,
        available: view.available,
        householdId: view.household_id ?? null,
        loading: false,
      })
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'failed to set scope',
        loading: false,
      })
    }
  },
}))
