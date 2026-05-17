// Scope values must match backend model.Scope* constants exactly.
export const SCOPE_PERSONAL = 'personal' as const
export const SCOPE_HOUSEHOLD = 'household' as const

export type Scope = typeof SCOPE_PERSONAL | typeof SCOPE_HOUSEHOLD

// ScopeView mirrors backend service.ScopeView.
export type ScopeView = {
  active: Scope
  available: Scope[]
  household_id?: number | null
}
