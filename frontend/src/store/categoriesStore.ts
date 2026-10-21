import { create } from 'zustand'
import { createCategory, deleteCategory, listCategories, renameCategory } from '../api/categories'
import type { Category, CategoryUsage } from '../types/category'

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
  // remove deletes or merges a category (#575), then reloads the list:
  // subcategories may have moved under the replacement.
  remove: (id: number, replaceWith: number | 'none') => Promise<CategoryUsage>
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
  remove: async (id, replaceWith) => {
    const moved = await deleteCategory(id, replaceWith)
    set({ categories: get().categories.filter((x) => x.id !== id) })
    try {
      set({ categories: await listCategories() })
    } catch {
      // The delete succeeded; a stale parent label is all that's left.
    }
    return moved
  },
}))
