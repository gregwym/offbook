import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  Budget,
  BudgetSpend,
  CreateBudgetInput,
  UpdateBudgetInput,
} from '../types/budget'

export async function listBudgets(): Promise<Budget[]> {
  const res = await apiClient.get<ApiList<Budget>>('/budgets')
  return res.data.data
}

export async function createBudget(input: CreateBudgetInput): Promise<Budget> {
  const res = await apiClient.post<ApiItem<Budget>>('/budgets', input)
  return res.data.data
}

export async function updateBudget(id: number, input: UpdateBudgetInput): Promise<Budget> {
  const res = await apiClient.patch<ApiItem<Budget>>(`/budgets/${id}`, input)
  return res.data.data
}

export async function deleteBudget(id: number): Promise<void> {
  await apiClient.delete(`/budgets/${id}`)
}

// on (YYYY-MM-DD) picks the period containing that date (#577); omit it for
// the current period.
export async function getBudgetSpend(id: number, on?: string): Promise<BudgetSpend> {
  const res = await apiClient.get<ApiItem<BudgetSpend>>(`/budgets/${id}/spend`, on ? { params: { on } } : undefined)
  return res.data.data
}
