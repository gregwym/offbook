// Mirrors backend/internal/model/categorization_rule.go.
export type MatchType = 'contains' | 'regex' | 'exact'

export const MATCH_TYPES: MatchType[] = ['contains', 'regex', 'exact']

export type CategorizationRule = {
  id: number
  user_id: number
  pattern: string
  category_id: number
  match_type: MatchType
  priority: number
  is_active: boolean
  created_at: string
  updated_at: string
}

export type CreateRuleInput = {
  pattern: string
  match_type: MatchType
  category_id: number
  priority: number
  is_active?: boolean
}

export type UpdateRuleInput = Partial<CreateRuleInput>

// Mirrors service.ApplyResult — three counts returned by POST /apply.
export type ApplyResult = {
  scanned: number
  updated: number
  skipped_manual: number
}

export type ApplyScope = 'all' | 'uncategorized' | 'plaid_default'
