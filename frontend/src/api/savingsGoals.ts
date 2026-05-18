import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  ContributionInput,
  CreateGoalInput,
  SavingsGoal,
  UpdateGoalInput,
} from '../types/savingsGoal'

export async function listGoals(): Promise<SavingsGoal[]> {
  const res = await apiClient.get<ApiList<SavingsGoal>>('/savings-goals')
  return res.data.data
}

export async function createGoal(input: CreateGoalInput): Promise<SavingsGoal> {
  const res = await apiClient.post<ApiItem<SavingsGoal>>('/savings-goals', input)
  return res.data.data
}

export async function updateGoal(id: number, input: UpdateGoalInput): Promise<SavingsGoal> {
  const res = await apiClient.patch<ApiItem<SavingsGoal>>(`/savings-goals/${id}`, input)
  return res.data.data
}

export async function deleteGoal(id: number): Promise<void> {
  await apiClient.delete(`/savings-goals/${id}`)
}

export async function contributeToGoal(id: number, input: ContributionInput): Promise<SavingsGoal> {
  const res = await apiClient.post<ApiItem<SavingsGoal>>(`/savings-goals/${id}/contributions`, input)
  return res.data.data
}
