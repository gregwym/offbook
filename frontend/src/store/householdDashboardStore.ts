import { create } from 'zustand'
import { getHouseholdDashboard } from '../api/householdAggregator'
import type { HouseholdDashboard, HouseholdPeriodKey } from '../types/householdAggregator'

type State = {
  dashboard: HouseholdDashboard | null
  period: HouseholdPeriodKey
  loading: boolean
  error: string | null
  errorCode: string | null

  load: () => Promise<void>
  setPeriod: (p: HouseholdPeriodKey) => Promise<void>
  clearError: () => void
}

export const useHouseholdDashboardStore = create<State>((set, get) => ({
  dashboard: null,
  period: 'current_month',
  loading: false,
  error: null,
  errorCode: null,

  load: async () => {
    set({ loading: true, error: null, errorCode: null })
    try {
      const d = await getHouseholdDashboard(get().period)
      set({ dashboard: d, loading: false })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  setPeriod: async (p) => {
    set({ period: p })
    await get().load()
  },

  clearError: () => set({ error: null, errorCode: null }),
}))

function errInfo(err: unknown): { error: string; errorCode: string | null } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { error: r.data.error, errorCode: r.data.code ?? null }
  }
  if (err instanceof Error) return { error: err.message, errorCode: null }
  return { error: 'request failed', errorCode: null }
}
