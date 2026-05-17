import { create } from 'zustand'
import {
  createAccount as apiCreate,
  deleteAccount as apiDelete,
  listAccounts,
  updateAccount as apiUpdate,
  type AccountListFilter,
} from '../api/accounts'
import type { Account, CreateAccountInput, UpdateAccountInput } from '../types/account'

type State = {
  accounts: Account[]
  total: number
  loading: boolean
  error: string | null
  filter: AccountListFilter
  fetch: (f?: AccountListFilter) => Promise<void>
  create: (input: CreateAccountInput) => Promise<Account>
  update: (id: number, input: UpdateAccountInput) => Promise<Account>
  remove: (id: number) => Promise<void>
}

export const useAccountsStore = create<State>((set, get) => ({
  accounts: [],
  total: 0,
  loading: false,
  error: null,
  filter: {},

  fetch: async (f) => {
    const filter = f ?? get().filter
    set({ loading: true, error: null, filter })
    try {
      const { items, total } = await listAccounts(filter)
      set({ accounts: items, total, loading: false })
    } catch (err) {
      set({ loading: false, error: errMsg(err) })
    }
  },

  create: async (input) => {
    set({ loading: true, error: null })
    try {
      const acct = await apiCreate(input)
      // Refetch instead of splicing: server-side ordering + filtering wins.
      await get().fetch()
      return acct
    } catch (err) {
      set({ loading: false, error: errMsg(err) })
      throw err
    }
  },

  update: async (id, input) => {
    set({ error: null })
    try {
      const acct = await apiUpdate(id, input)
      set({ accounts: get().accounts.map((a) => (a.id === id ? acct : a)) })
      return acct
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  remove: async (id) => {
    set({ error: null })
    try {
      await apiDelete(id)
      set({ accounts: get().accounts.filter((a) => a.id !== id) })
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
