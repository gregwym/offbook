import { apiClient, type ApiItem } from './client'
import type { AuthUser, CurrentUser, SetupStatus, SignupMode } from '../types/auth'

export async function getSetupStatus(): Promise<SetupStatus> {
  const res = await apiClient.get<ApiItem<SetupStatus>>('/setup/status')
  return res.data.data
}

export async function setupAdmin(input: {
  email: string
  password: string
  signup_mode: SignupMode
}): Promise<AuthUser> {
  const res = await apiClient.post<ApiItem<AuthUser>>('/setup/admin', input)
  return res.data.data
}

export async function signin(email: string, password: string): Promise<AuthUser> {
  const res = await apiClient.post<ApiItem<AuthUser>>('/auth/signin', { email, password })
  return res.data.data
}

export async function signup(email: string, password: string): Promise<AuthUser> {
  const res = await apiClient.post<ApiItem<AuthUser>>('/auth/signup', { email, password })
  return res.data.data
}

export async function signout(): Promise<void> {
  await apiClient.post('/auth/signout')
}

export async function getMe(): Promise<CurrentUser> {
  const res = await apiClient.get<ApiItem<CurrentUser>>('/me')
  return res.data.data
}
