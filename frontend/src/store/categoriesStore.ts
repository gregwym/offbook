import { create } from 'zustand'
import { createCategory, listCategories, renameCategory } from '../api/categories'
import type { Category } from '../types/category'

type State = {
  categories: Category[]
  loaded: boolean
  loading: boolean
  error: string | null
  fetch: () => Promise<void>
  // create/rename write through the API and update the cached list, so every
  // category picker sees the change without a reload (#573). Errors throw to
  // the caller, which shows them inline.
  create: (input: { name: string; parent_id?: number | null }) => Promise<Category>
  rename: (id: number, name: string) => Promise<Category>
}

const byName = (a: Category, b: Category) => a.name.localeCompare(b.name)

// Cached once per session; local edits keep the cache current.
export const useCategoriesStore = create<State>((set, get) => ({
  categories: [],
  loaded: false,
  loading: false,
  error: null,
  fetch: async () => {
    if (get().loaded || get().loading) return
    set({ loading: true, error: null })
    try {
      const cats = await listCategories()
      set({ categories: cats, loaded: true, loading: false })
    } catch (err) {
      set({ loading: false, error: err instanceof Error ? err.message : 'failed' })
    }
  },
  create: async (input) => {
    const c = await createCategory(input)
    set({ categories: [...get().categories, c].sort(byName) })
    return c
  },
  rename: async (id, name) => {
    const c = await renameCategory(id, name)
    set({ categories: get().categories.map((x) => (x.id === id ? c : x)).sort(byName) })
    return c
  },
}))
