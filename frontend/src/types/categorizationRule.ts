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
  // Merchant name written to every transaction this rule wins (#534).
  rename_merchant?: string | null
  // Hides every flow transaction this rule wins from spending, budgets and
  // merchant reports (#536). Never unhides.
  hide_matches?: boolean
  created_at: string
  updated_at: string
}

export type CreateRuleInput = {
  pattern: string
  match_type: MatchType
  category_id: number
  priority: number
  is_active?: boolean
  // Blank clears the rename on update (#534).
  rename_merchant?: string
  hide_matches?: boolean
}

export type UpdateRuleInput = Partial<CreateRuleInput>

// Mirrors service.ApplyResult — three counts returned by POST /apply.
export type ApplyResult = {
  scanned: number
  updated: number
  skipped_manual: number
  // true when nothing was written; `updated` is then the would-change count.
  dry_run?: boolean
}

export type ApplyScope = 'all' | 'uncategorized' | 'plaid_default'

// Mirrors service.DraftPreview — POST /categorization-rules/preview.
export type DraftPreview = {
  scanned: number
  matched: number
  would_change: number
  // Matched rows whose merchant the draft's rename would change (#534).
  would_rename: number
  // Matched rows the draft would hide (#536).
  would_hide: number
  skipped_manual: number
  samples: string[]
}
