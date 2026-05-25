import { apiClient, type ApiItem, type ApiList } from './client'
import type { Asset, AssetKind, EnsureAssetInput } from '../types/asset'

export async function listAssets(kind?: AssetKind): Promise<Asset[]> {
  const params: Record<string, string> = {}
  if (kind) params.kind = kind
  const res = await apiClient.get<ApiList<Asset>>('/assets', { params })
  return res.data.data
}

export async function ensureAsset(input: EnsureAssetInput): Promise<Asset> {
  const res = await apiClient.post<ApiItem<Asset>>('/assets/ensure', input)
  return res.data.data
}
