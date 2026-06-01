import { apiClient, type ApiItem } from './client'

// HealthInfo mirrors the /health envelope's data object. `version` is the
// build's short git SHA (or "dev" for un-stamped local builds) — surfaced in
// Settings so deploy freshness is verifiable without shell access (#310).
export type HealthInfo = {
  status: string
  version?: string
}

// getHealth reads the public health endpoint. The backend returns the same
// data shape (including `version`) on both the 200 and 503 paths; callers
// that only want the version can ignore `status`.
export async function getHealth(): Promise<HealthInfo> {
  const res = await apiClient.get<ApiItem<HealthInfo>>('/health')
  return res.data.data
}
