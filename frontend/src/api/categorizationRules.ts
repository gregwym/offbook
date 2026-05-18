import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  ApplyResult,
  ApplyScope,
  CategorizationRule,
  CreateRuleInput,
  UpdateRuleInput,
} from '../types/categorizationRule'

export async function listRules(): Promise<CategorizationRule[]> {
  const res = await apiClient.get<ApiList<CategorizationRule>>('/categorization-rules')
  return res.data.data
}

export async function createRule(input: CreateRuleInput): Promise<CategorizationRule> {
  const res = await apiClient.post<ApiItem<CategorizationRule>>('/categorization-rules', input)
  return res.data.data
}

export async function updateRule(id: number, input: UpdateRuleInput): Promise<CategorizationRule> {
  const res = await apiClient.patch<ApiItem<CategorizationRule>>(`/categorization-rules/${id}`, input)
  return res.data.data
}

export async function deleteRule(id: number): Promise<void> {
  await apiClient.delete(`/categorization-rules/${id}`)
}

// Empty body defaults to scope='all' on the backend.
export async function applyRules(scope?: ApplyScope): Promise<ApplyResult> {
  const body = scope ? { scope } : {}
  const res = await apiClient.post<ApiItem<ApplyResult>>('/categorization-rules/apply', body)
  return res.data.data
}
