import { apiClient, type ApiItem, type ApiList } from './client'
import type { Category, CategoryUsage } from '../types/category'

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

// What deleting or merging the category would move (#575).
export async function getCategoryUsage(id: number): Promise<CategoryUsage> {
  const res = await apiClient.get<ApiItem<CategoryUsage>>(`/categories/${id}/usage`)
  return res.data.data
}

// Deletes the user's own category after moving everything that uses it to
// replaceWith ('none' = Uncategorized). Moving to a category merges the two.
export async function deleteCategory(id: number, replaceWith: number | 'none'): Promise<CategoryUsage> {
  const res = await apiClient.delete<ApiItem<CategoryUsage>>(`/categories/${id}`, {
    params: { replace_with: replaceWith },
  })
  return res.data.data
}
