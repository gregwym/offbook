// Mirror of backend service/household types. Keep aligned with
// backend/internal/model/household.go and service/household/service.go.

export type HouseholdRole = 'owner' | 'contributor' | 'view_only'

export type Household = {
  id: number
  name: string
  owner_id: number
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
