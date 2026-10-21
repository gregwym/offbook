import { apiClient, type ApiItem, type ApiList } from './client'
import type { Category } from '../types/category'

// System categories plus the caller's own (#573).
export async function listCategories(): Promise<Category[]> {
  const res = await apiClient.get<ApiList<Category>>('/categories')
  return res.data.data
}

export async function createCategory(input: { name: string; parent_id?: number | null }): Promise<Category> {
  const res = await apiClient.post<ApiItem<Category>>('/categories', input)
  return res.data.data
}

export async function renameCategory(id: number, name: string): Promise<Category> {
  const res = await apiClient.patch<ApiItem<Category>>(`/categories/${id}`, { name })
  return res.data.data
}
