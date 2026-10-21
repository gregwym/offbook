// Mirrors backend/internal/model/categorization_rule.go.
export type MatchType = 'contains' | 'regex' | 'exact'

export const MATCH_TYPES: MatchType[] = ['contains', 'regex', 'exact']

export type CategorizationRule = {
  id: number
  user_id: number
  pattern: string
  // null: the rule keeps each matched transaction's category and only takes
  // its other actions (#553).
  category_id: number | null
  match_type: MatchType
  priority: number
  is_active: boolean
  // Merchant name written to every transaction this rule wins (#534).
  rename_merchant?: string | null
  // Hides every flow transaction this rule wins from spending, budgets and
  // merchant reports (#536). Never unhides.
  hide_matches?: boolean
  // Marks every flow transaction this rule wins reviewed, so it skips the
  // Needs review inbox (#538). Never un-reviews.
  mark_reviewed?: boolean
  // Tags added to every flow transaction this rule wins, skipping ones it
  // already has (#546). Never removes a tag.
  add_tags?: string[]
  // Conditions ANDed with the pattern (#550): only this account, and only
  // when |amount| is within [amount_min, amount_max] (decimal strings,
  // inclusive). Absent = no condition.
  account_id?: number | null
  amount_min?: string | null
  amount_max?: string | null
  created_at: string
  updated_at: string
}

export type CreateRuleInput = {
  pattern: string
  match_type: MatchType
  // Omit to keep each matched transaction's category (#553); the rule then
  // needs another action.
  category_id?: number
  priority: number
  is_active?: boolean
  // Blank clears the rename on update (#534).
  rename_merchant?: string
  hide_matches?: boolean
  mark_reviewed?: boolean
  // [] clears the rule's tags on update (#546).
  add_tags?: string[]
  // Optional conditions (#550); amounts are non-negative decimal strings.
  account_id?: number
  amount_min?: string
  amount_max?: string
}

export type UpdateRuleInput = Partial<CreateRuleInput> & {
  // Switch the rule to keeping each transaction's category (#553).
  clear_category_id?: boolean
  // Drop a condition on update (#550).
  clear_account_id?: boolean
  clear_amount_min?: boolean
  clear_amount_max?: boolean
}

// Mirrors service.ApplyResult — three counts returned by POST /apply.
export type ApplyResult = {
  scanned: number
  updated: number
  skipped_manual: number
  // Per-action subsets of `updated` (#552); a row counts once per action.
  recategorized?: number
  renamed?: number
  hidden?: number
  marked_reviewed?: number
  tagged?: number
  // true when nothing was written; `updated` and the per-action counts then
  // describe what would change.
  dry_run?: boolean
  // Id of the committed run, for undo (#565). Absent on a dry run and when
  // nothing changed.
  run_id?: number
}

// Mirrors model.RuleApplyRun — a committed apply run that can be undone
// (#565). rows_restored/rows_kept are set once it is undone.
export type RuleApplyRun = {
  id: number
  scope: ApplyScope
  rows_changed: number
  created_at: string
  undone_at?: string
  rows_restored?: number
  rows_kept?: number
}

export type ApplyScope = 'all' | 'uncategorized' | 'plaid_default'

// Mirrors service.DraftPreview — POST /categorization-rules/preview.
export type DraftPreview = {
  scanned: number
  matched: number
  would_change: number
  // Matched rows whose category the draft would change (#552).
  would_recategorize: number
  // Matched rows whose merchant the draft's rename would change (#534).
  would_rename: number
  // Matched rows the draft would hide (#536).
  would_hide: number
  // Matched rows the draft would mark reviewed (#538).
  would_mark_reviewed: number
  // Matched rows that would gain at least one of the draft's tags (#546).
  would_tag: number
  skipped_manual: number
  samples: string[]
}
