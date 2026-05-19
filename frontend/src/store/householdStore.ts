import { create } from 'zustand'
import {
  createInvite as apiCreateInvite,
  getHousehold,
  leaveHousehold as apiLeave,
  updateHousehold as apiUpdate,
} from '../api/households'
import type { CreateInviteResult, HouseholdDetail, HouseholdRole } from '../types/household'

type State = {
  detail: HouseholdDetail | null
  loading: boolean
  error: string | null
  lastInvite: CreateInviteResult | null

  load: (id: number) => Promise<void>
  refresh: (id: number) => Promise<void>
  invite: (id: number, role: HouseholdRole) => Promise<CreateInviteResult>
  clearInvite: () => void
  updateHousehold: (id: number, patch: { name?: string; grace_period_days?: number }) => Promise<void>
  leave: (id: number) => Promise<void>
  clearError: () => void
}

export const useHouseholdStore = create<State>((set, get) => ({
  detail: null,
  loading: false,
  error: null,
  lastInvite: null,

  load: async (id) => {
    set({ loading: true, error: null, detail: null })
    try {
      const d = await getHousehold(id)
      set({ detail: d, loading: false })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  refresh: async (id) => {
    // Same as load but doesn't clear the cached detail — used after a
    // mutation so the page doesn't blank out mid-render.
    try {
      const d = await getHousehold(id)
      set({ detail: d })
    } catch (err) {
      set(errInfo(err))
    }
  },

  invite: async (id, role) => {
    set({ error: null })
    try {
      const res = await apiCreateInvite(id, role)
      set({ lastInvite: res })
      return res
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  clearInvite: () => set({ lastInvite: null }),

  updateHousehold: async (id, patch) => {
    set({ error: null })
    try {
      await apiUpdate(id, patch)
      await get().refresh(id)
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  leave: async (id) => {
    set({ error: null })
    try {
      await apiLeave(id)
      set({ detail: null })
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  clearError: () => set({ error: null }),
}))

function errInfo(err: unknown): { error: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return { error: r.data.error }
  }
  if (err instanceof Error) return { error: err.message }
  return { error: 'request failed' }
}
