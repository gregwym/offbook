import axios from 'axios'

// Vite proxies /api → backend in dev (see vite.config.ts). In other
// environments (preview build, prod, container) set VITE_API_BASE_URL.
const baseURL = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

// withCredentials: true so the session cookie set by /auth/* and /setup/admin
// is sent on every subsequent request. The vite proxy preserves Set-Cookie /
// Cookie headers, and the backend's CORS middleware sets Allow-Credentials.
export const apiClient = axios.create({
  baseURL,
  headers: { 'Content-Type': 'application/json' },
  withCredentials: true,
})

// onUnauthorized is called when any API request returns 401. The auth
// store registers a handler at boot to clear the user + redirect to
// /signin. Module-level rather than a direct store import to avoid the
// circular dep (client → store → api/auth → client).
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

apiClient.interceptors.response.use(
  (res) => res,
  (err) => {
    const url: string = err?.config?.url ?? ''
    const status: number | undefined = err?.response?.status
    if (
      status === 401 &&
      // Auth-probe and signin endpoints expect 401s — handling them here
      // would loop the user back to signin from inside signin.
      !url.includes('/auth/signin') &&
      !url.includes('/auth/signup') &&
      !url.includes('/setup/') &&
      !url.includes('/me') // hydrate() probes /me and handles 401 itself
    ) {
      onUnauthorized?.()
    }
    return Promise.reject(err)
  },
)

// Standard backend envelope shapes (mirror backend handler conventions).
export type ApiList<T> = { data: T[]; total: number }
export type ApiItem<T> = { data: T }
export type ApiError = { error: string; code?: string }
