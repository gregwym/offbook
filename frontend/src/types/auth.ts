// Mirror of backend auth response shapes. See backend/internal/handler/auth_handler.go.

export type SignupMode = 'invite_only' | 'local_multi_tenant'

export type SetupStatus = {
  bootstrapped: boolean
  signup_mode: SignupMode | null
}

export type AuthUser = {
  id: number
  email?: string
  is_admin?: boolean
}

// CurrentUser is what `GET /me` returns. The backend currently returns
// `{id, user_id}` — both alias the session user. Extend as backend
// surfaces more.
export type CurrentUser = {
  id: number
  user_id: number
}
