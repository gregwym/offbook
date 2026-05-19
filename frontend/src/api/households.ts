import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  CreateInviteResult,
  CreateSharedBudgetInput,
  CreateSharedGoalInput,
  Household,
  HouseholdDetail,
  HouseholdMember,
  HouseholdRole,
  MembersListing,
  SharedBudget,
  SharedGoal,
  UpdateSharedBudgetInput,
  UpdateSharedGoalInput,
} from '../types/household'

export async function getHousehold(id: number): Promise<HouseholdDetail> {
  const res = await apiClient.get<ApiItem<HouseholdDetail>>(`/households/${id}`)
  return res.data.data
}

export async function createHousehold(name: string): Promise<Household> {
  const res = await apiClient.post<ApiItem<Household>>('/households', { name })
  return res.data.data
}

export async function updateHousehold(
  id: number,
  input: { name?: string; grace_period_days?: number },
): Promise<Household> {
  const res = await apiClient.patch<ApiItem<Household>>(`/households/${id}`, input)
  return res.data.data
}

export async function createInvite(
  householdID: number,
  role: HouseholdRole,
): Promise<CreateInviteResult> {
  const res = await apiClient.post<ApiItem<CreateInviteResult>>(
    `/households/${householdID}/invites`,
    { role },
  )
  return res.data.data
}

export async function acceptInvite(token: string): Promise<unknown> {
  const res = await apiClient.post<ApiItem<unknown>>(`/invites/${encodeURIComponent(token)}/accept`)
  return res.data.data
}

export async function leaveHousehold(id: number): Promise<void> {
  await apiClient.delete(`/households/${id}/members/me`)
}

export async function listMembers(
  householdID: number,
  includeInGrace: boolean,
): Promise<MembersListing> {
  const res = await apiClient.get<ApiItem<MembersListing>>(`/households/${householdID}/members`, {
    params: includeInGrace ? { include: 'in_grace' } : undefined,
  })
  return res.data.data
}

export async function updateMemberRole(
  householdID: number,
  userID: number,
  role: HouseholdRole,
): Promise<HouseholdMember> {
  const res = await apiClient.patch<ApiItem<HouseholdMember>>(
    `/households/${householdID}/members/${userID}`,
    { role },
  )
  return res.data.data
}

export async function removeMember(householdID: number, userID: number): Promise<void> {
  await apiClient.delete(`/households/${householdID}/members/${userID}`)
}

export async function transferOwner(householdID: number, userID: number): Promise<void> {
  await apiClient.post(`/households/${householdID}/transfer-owner`, { user_id: userID })
}

export async function listSharedBudgets(householdID: number): Promise<SharedBudget[]> {
  const res = await apiClient.get<ApiList<SharedBudget>>(`/households/${householdID}/shared-budgets`)
  return res.data.data
}

export async function createSharedBudget(
  householdID: number,
  input: CreateSharedBudgetInput,
): Promise<SharedBudget> {
  const res = await apiClient.post<ApiItem<SharedBudget>>(
    `/households/${householdID}/shared-budgets`,
    input,
  )
  return res.data.data
}

export async function updateSharedBudget(
  householdID: number,
  budgetID: number,
  input: UpdateSharedBudgetInput,
): Promise<SharedBudget> {
  const res = await apiClient.patch<ApiItem<SharedBudget>>(
    `/households/${householdID}/shared-budgets/${budgetID}`,
    input,
  )
  return res.data.data
}

export async function deleteSharedBudget(householdID: number, budgetID: number): Promise<void> {
  await apiClient.delete(`/households/${householdID}/shared-budgets/${budgetID}`)
}

export async function listSharedGoals(householdID: number): Promise<SharedGoal[]> {
  const res = await apiClient.get<ApiList<SharedGoal>>(`/households/${householdID}/shared-goals`)
  return res.data.data
}

export async function createSharedGoal(
  householdID: number,
  input: CreateSharedGoalInput,
): Promise<SharedGoal> {
  const res = await apiClient.post<ApiItem<SharedGoal>>(
    `/households/${householdID}/shared-goals`,
    input,
  )
  return res.data.data
}

export async function updateSharedGoal(
  householdID: number,
  goalID: number,
  input: UpdateSharedGoalInput,
): Promise<SharedGoal> {
  const res = await apiClient.patch<ApiItem<SharedGoal>>(
    `/households/${householdID}/shared-goals/${goalID}`,
    input,
  )
  return res.data.data
}

export async function deleteSharedGoal(householdID: number, goalID: number): Promise<void> {
  await apiClient.delete(`/households/${householdID}/shared-goals/${goalID}`)
}

export async function contributeToSharedGoal(
  householdID: number,
  goalID: number,
  amount: string,
): Promise<SharedGoal> {
  const res = await apiClient.post<ApiItem<SharedGoal>>(
    `/households/${householdID}/shared-goals/${goalID}/contributions`,
    { amount },
  )
  return res.data.data
}
