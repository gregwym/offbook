import { apiClient, type ApiItem, type ApiList } from './client'
import type {
  ApplyResult,
  ApplyScope,
  CategorizationRule,
  CreateRuleInput,
  DraftPreview,
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
// dryRun reports what would change without writing.
export async function applyRules(scope?: ApplyScope, dryRun?: boolean): Promise<ApplyResult> {
  const body = { ...(scope ? { scope } : {}), ...(dryRun ? { dry_run: true } : {}) }
  const res = await apiClient.post<ApiItem<ApplyResult>>('/categorization-rules/apply', body)
  return res.data.data
}

// Impact of an unsaved rule (ruleId set when editing); writes nothing.
export async function previewDraftRule(
  input: CreateRuleInput,
  ruleId?: number,
  signal?: AbortSignal,
): Promise<DraftPreview> {
  const res = await apiClient.post<ApiItem<DraftPreview>>(
    '/categorization-rules/preview',
    { ...input, ...(ruleId ? { rule_id: ruleId } : {}) },
    { signal },
  )
  return res.data.data
}
