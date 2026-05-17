import { create } from 'zustand'
import { listCategories } from '../api/categories'
import type { Category } from '../types/category'

type State = {
  categories: Category[]
  loaded: boolean
  loading: boolean
  error: string | null
  fetch: () => Promise<void>
}

// Categories are seeded lookup data — cache once per session.
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
}))
