import { create } from 'zustand'
import {
  getMe,
  getSetupStatus,
  setupAdmin as apiSetupAdmin,
  signin as apiSignin,
  signout as apiSignout,
  signup as apiSignup,
  signupWithInvite as apiSignupWithInvite,
} from '../api/auth'
import type { AuthUser, SetupStatus, SignupMode } from '../types/auth'

// AuthState is the canonical "who am I, can I sign in here" client store.
// AppShell consults it before rendering protected routes; the auth pages
// consume it to trigger the actual signin/signup/setup calls.
type AuthState = {
  // Setup probe — null until hydrated; afterward the shape from the backend.
  setup: SetupStatus | null
  // Current user — null when unauthenticated. Distinct from setup so we can
  // render `/setup/admin` when bootstrapped=false even without a session.
  user: AuthUser | null
  // hydrated flips true on the first round-trip so the app can stop
  // showing "Loading…" the moment we know the auth state.
  hydrated: boolean
  error: string | null

  hydrate: () => Promise<void>
  setupAdmin: (email: string, password: string, mode: SignupMode) => Promise<void>
  signin: (email: string, password: string) => Promise<void>
  signup: (email: string, password: string) => Promise<void>
  signupWithInvite: (email: string, password: string, token: string) => Promise<void>
  signout: () => Promise<void>
  clearError: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  setup: null,
  user: null,
  hydrated: false,
  error: null,

  hydrate: async () => {
    // Resolve setup status first so we can decide where to send the user
    // when /me is also unauthenticated. Both probes are unauthenticated
    // (setup) or 401-tolerant (me).
    let status: SetupStatus
    try {
      status = await getSetupStatus()
    } catch (err) {
      // /setup/status failing means the server's unreachable. The auth
      // pages render a friendly "server unavailable" state via `error`.
      set({
        setup: null,
        user: null,
        hydrated: true,
        error: err instanceof Error ? err.message : 'auth probe failed',
      })
      return
    }

    let user: AuthUser | null = null
    if (status.bootstrapped) {
      try {
        const me = await getMe()
        user = { id: me.id }
      } catch {
        // 401 / 403 — unauthenticated. Not an error condition, just means
        // we need the user to sign in.
        user = null
      }
    }

    set({ setup: status, user, hydrated: true, error: null })
  },

  setupAdmin: async (email, password, mode) => {
    set({ error: null })
    try {
      const u = await apiSetupAdmin({ email, password, signup_mode: mode })
      set({
        user: u,
        setup: { bootstrapped: true, signup_mode: mode },
      })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  signin: async (email, password) => {
    set({ error: null })
    try {
      const u = await apiSignin(email, password)
      set({ user: u })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  signup: async (email, password) => {
    set({ error: null })
    try {
      const u = await apiSignup(email, password)
      set({ user: u })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  signupWithInvite: async (email, password, token) => {
    set({ error: null })
    try {
      const u = await apiSignupWithInvite(email, password, token)
      set({ user: u })
    } catch (err) {
      set({ error: errMsg(err) })
      throw err
    }
  },

  signout: async () => {
    try {
      await apiSignout()
    } catch {
      // Best-effort — server might already have deleted the session.
    }
    set({ user: null })
  },

  clearError: () => set({ error: null }),
}))

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
