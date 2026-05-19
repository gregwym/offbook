import { apiClient, type ApiItem } from './client'
import type {
  CreateInviteResult,
  Household,
  HouseholdDetail,
  HouseholdRole,
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
