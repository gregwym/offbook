// Mirror of backend service/household types. Keep aligned with
// backend/internal/model/household.go and service/household/service.go.

export type HouseholdRole = 'owner' | 'contributor' | 'view_only'

export type Household = {
  id: number
  name: string
  // Ownership is not a household field — it's the member with role 'owner'
  // (single source of truth, backend #283).
  grace_period_days: number
  created_at: string
  updated_at: string
}

export type HouseholdMember = {
  id: number
  household_id: number
  user_id: number
  role: HouseholdRole
  joined_at: string
  left_at?: string | null
  purged_at?: string | null
}

// HouseholdDetail mirrors service/household.HouseholdDetail.
export type HouseholdDetail = {
  household: Household
  members: HouseholdMember[]
  role: HouseholdRole // requester's role in this household
}

// MembersListing mirrors service/household.MembersListing — the listing
// endpoint returns active members + (optionally) in-grace members.
export type MembersListing = {
  active: HouseholdMember[]
  in_grace?: HouseholdMember[] | null
}

export type HouseholdInvite = {
  id: number
  household_id: number
  role: HouseholdRole
  created_by: number
  expires_at: string
  accepted_at?: string | null
  accepted_by?: number | null
  created_at: string
}

// CreateInviteResult mirrors service/household.CreateInviteResult — the
// raw token is only returned at creation time.
export type CreateInviteResult = {
  invite: HouseholdInvite
  token: string
}

// SharedBudget mirrors backend model.SharedBudget.
export type SharedBudget = {
  id: number
  household_id: number
  category_id: number
  period: 'monthly' | 'weekly' | 'annual'
  amount: string
  rollover: boolean
  is_active: boolean
  created_at: string
  updated_at: string
}

export type CreateSharedBudgetInput = {
  category_id: number
  period: 'monthly' | 'weekly' | 'annual'
  amount: string
  rollover?: boolean
  is_active?: boolean
}

export type UpdateSharedBudgetInput = {
  category_id?: number
  period?: 'monthly' | 'weekly' | 'annual'
  amount?: string
  rollover?: boolean
  is_active?: boolean
}

// SharedGoal mirrors backend model.SharedGoal.
export type SharedGoal = {
  id: number
  household_id: number
  name: string
  target_amount: string
  current_amount: string
  target_date?: string | null
  created_at: string
  updated_at: string
}

export type CreateSharedGoalInput = {
  name: string
  target_amount: string
  target_date?: string // YYYY-MM-DD
}

export type UpdateSharedGoalInput = {
  name?: string
  target_amount?: string
  target_date?: string
  clear_target_date?: boolean
}
