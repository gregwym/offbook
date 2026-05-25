import { create } from 'zustand'
import { ensureAsset as apiEnsure, listAssets } from '../api/assets'
import type { Asset, EnsureAssetInput } from '../types/asset'

type State = {
  assets: Asset[]
  loaded: boolean
  loading: boolean
  error: string | null
  fetch: () => Promise<void>
  // ensure adds the returned asset to the cache when it wasn't there yet.
  ensure: (input: EnsureAssetInput) => Promise<Asset>
}

// Assets are global reference data — cache once per session and refresh
// on ensure-create.
export const useAssetsStore = create<State>((set, get) => ({
  assets: [],
  loaded: false,
  loading: false,
  error: null,
  fetch: async () => {
    if (get().loaded || get().loading) return
    set({ loading: true, error: null })
    try {
      const assets = await listAssets()
      set({ assets, loaded: true, loading: false })
    } catch (err) {
      set({ loading: false, error: err instanceof Error ? err.message : 'failed' })
    }
  },
  ensure: async (input) => {
    const a = await apiEnsure(input)
    const existing = get().assets.find((x) => x.id === a.id)
    if (!existing) {
      set({ assets: [...get().assets, a] })
    }
    return a
  },
}))
