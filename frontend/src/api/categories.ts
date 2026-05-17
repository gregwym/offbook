import { apiClient, type ApiList } from './client'
import type { Category } from '../types/category'

// Categories are seeded lookup data — single GET, no filters.
export async function listCategories(): Promise<Category[]> {
  const res = await apiClient.get<ApiList<Category>>('/categories')
  return res.data.data
}
