import { create } from 'zustand'
import {
  createInvestment as apiCreate,
  getPortfolioSummary,
  listLatestHoldings,
  listSnapshotHistory,
} from '../api/investments'
import type {
  CreateInvestmentInput,
  Investment,
  PortfolioSummary,
} from '../types/investment'

type State = {
  holdings: Investment[]
  portfolio: PortfolioSummary | null
  loading: boolean
  error: string | null
  code: string | null
  fetch: () => Promise<void>
  create: (input: CreateInvestmentInput) => Promise<Investment>
  fetchHistory: (accountID: number, ticker: string) => Promise<Investment[]>
  clearError: () => void
}

// Investments are append-only — no update/delete here. fetch() loads
// holdings + portfolio in parallel; the page surfaces both.
export const useInvestmentsStore = create<State>((set, get) => ({
  holdings: [],
  portfolio: null,
  loading: false,
  error: null,
  code: null,

  fetch: async () => {
    set({ loading: true, error: null, code: null })
    try {
      const [holdings, portfolio] = await Promise.all([
        listLatestHoldings(),
        getPortfolioSummary(),
      ])
      set({ holdings, portfolio, loading: false })
    } catch (err) {
      set({ loading: false, ...errInfo(err) })
    }
  },

  create: async (input) => {
    set({ error: null, code: null })
    try {
      const inv = await apiCreate(input)
      await get().fetch()
      return inv
    } catch (err) {
      set(errInfo(err))
      throw err
    }
  },

  fetchHistory: async (accountID, ticker) => {
    return listSnapshotHistory(accountID, ticker)
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
