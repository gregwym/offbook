import { apiClient, type ApiItem } from './client'
import type { UpdateUserSettingsInput, UserSettingsView } from '../types/userSettings'

export async function getUserSettings(): Promise<UserSettingsView> {
  const res = await apiClient.get<ApiItem<UserSettingsView>>('/me/settings')
  return res.data.data
}

export async function updateUserSettings(input: UpdateUserSettingsInput): Promise<UserSettingsView> {
  const res = await apiClient.patch<ApiItem<UserSettingsView>>('/me/settings', input)
  return res.data.data
}
