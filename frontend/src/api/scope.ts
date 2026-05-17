import { apiClient, type ApiItem } from './client'
import type { Scope, ScopeView } from '../types/scope'

// GET /me/scope — current active scope + the set the user can switch to.
export async function getScope(): Promise<ScopeView> {
  const res = await apiClient.get<ApiItem<ScopeView>>('/me/scope')
  return res.data.data
}

// PATCH /me/scope — persists the new scope on users.last_scope.
// Returns the canonical ScopeView the server settled on.
export async function setScope(scope: Scope): Promise<ScopeView> {
  const res = await apiClient.patch<ApiItem<ScopeView>>('/me/scope', { scope })
  return res.data.data
}
