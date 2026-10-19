// Plaid OAuth resume state (#502). OAuth institutions (most large US banks
// in production) send the browser away to the bank and back to
// /plaid/oauth-return as a full-page redirect, which wipes in-memory React
// state. Before opening Link we stash the link token here so the return page
// can re-initialize Link with it plus `receivedRedirectUri`.
//
// sessionStorage (not localStorage): scoped to the tab that started Link,
// gone when the tab closes. The link token is short-lived and only useful
// with this user's session; never log it. The public_token is never stored.

export const PLAID_OAUTH_STORAGE_KEY = 'offbook.plaid.pendingLink'

export type PendingPlaidLink = {
  // 'new' = Add account (exchange → sync); 'update' = Settings re-auth
  // (#364; no exchange, just resume syncs for itemId).
  mode: 'new' | 'update'
  // Null once Link has succeeded — a reload must not reopen Link with a
  // consumed token.
  linkToken: string | null
  // ISO timestamp from POST /plaid/link/token.
  expiration: string
  // Plaid item id: the re-auth target in update mode, or the newly linked
  // item after a successful exchange in new mode. Lets a reload resume the
  // syncs instead of exchanging twice.
  itemId?: string
}

export function savePendingLink(p: PendingPlaidLink): void {
  try {
    window.sessionStorage.setItem(PLAID_OAUTH_STORAGE_KEY, JSON.stringify(p))
  } catch {
    // Storage blocked (private mode, quota). Non-OAuth institutions still
    // work; an OAuth return will show the "start again" state.
  }
}

export function loadPendingLink(): PendingPlaidLink | null {
  try {
    const raw = window.sessionStorage.getItem(PLAID_OAUTH_STORAGE_KEY)
    if (!raw) return null
    const p = JSON.parse(raw) as Partial<PendingPlaidLink>
    if ((p.mode !== 'new' && p.mode !== 'update') || typeof p.expiration !== 'string') return null
    return {
      mode: p.mode,
      linkToken: typeof p.linkToken === 'string' ? p.linkToken : null,
      expiration: p.expiration,
      itemId: typeof p.itemId === 'string' ? p.itemId : undefined,
    }
  } catch {
    return null
  }
}

export function clearPendingLink(): void {
  try {
    window.sessionStorage.removeItem(PLAID_OAUTH_STORAGE_KEY)
  } catch {
    // ignore
  }
}

export function isLinkTokenExpired(p: PendingPlaidLink, now: Date = new Date()): boolean {
  const t = Date.parse(p.expiration)
  return Number.isNaN(t) || t <= now.getTime()
}
