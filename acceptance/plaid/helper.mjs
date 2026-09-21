import { acceptanceAPIURL, loadQAEnv } from '../fixtures/env.mjs'

loadQAEnv()

export async function plaidSandboxPublicToken() {
  const clientID = process.env.PLAID_CLIENT_ID
  const secret = process.env.PLAID_SECRET
  if (!clientID || !secret) {
    throw new Error('PLAID_CLIENT_ID and PLAID_SECRET are required for Plaid sandbox acceptance tests')
  }

  const res = await fetch('https://sandbox.plaid.com/sandbox/public_token/create', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      client_id: clientID,
      secret,
      institution_id: process.env.PLAID_SANDBOX_INSTITUTION_ID ?? 'ins_109508',
      initial_products: ['transactions'],
      options: {
        webhook: process.env.PLAID_SANDBOX_WEBHOOK,
      },
    }),
  })
  if (!res.ok) throw new Error(`Plaid sandbox token create failed: HTTP ${res.status} ${await res.text()}`)
  const body = await res.json()
  return body.public_token
}

export async function exchangePublicToken(cookie, publicToken) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/link/exchange`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', cookie },
    body: JSON.stringify({ public_token: publicToken }),
  })
  if (!res.ok) throw new Error(`Offbook Plaid exchange failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

export async function syncPlaidAccounts(cookie, plaidItemID) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/items/${encodeURIComponent(plaidItemID)}/sync-accounts`, {
    method: 'POST',
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook Plaid account sync failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

export async function syncPlaidTransactions(cookie, plaidItemID) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/items/${encodeURIComponent(plaidItemID)}/sync-transactions`, {
    method: 'POST',
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook Plaid transaction sync failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

export async function listAccounts(cookie) {
  const res = await fetch(`${acceptanceAPIURL()}/accounts?limit=200`, {
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook accounts list failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

export async function listTransactions(cookie) {
  const res = await fetch(`${acceptanceAPIURL()}/transactions?limit=500`, {
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook transactions list failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

export async function listPlaidItems(cookie) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/items`, {
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook Plaid items list failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}

// resetSandboxItemLogin forces plaidItemID into ITEM_LOGIN_REQUIRED via
// Plaid's sandbox-only /sandbox/item/reset_login, routed through Offbook's
// own sandbox-gated endpoint (#364) rather than calling Plaid directly —
// this exercises the same access_token lookup + ownership scoping the real
// reconnect flow uses.
export async function resetSandboxItemLogin(cookie, plaidItemID) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/items/${encodeURIComponent(plaidItemID)}/sandbox/reset-login`, {
    method: 'POST',
    headers: { cookie },
  })
  if (!res.ok) throw new Error(`Offbook sandbox reset-login failed: HTTP ${res.status} ${await res.text()}`)
}

// attemptSyncTransactions is like syncPlaidTransactions but returns the raw
// Response instead of throwing on a non-2xx status — used to observe the
// expected failure once an item's access_token has been forced stale.
export async function attemptSyncTransactions(cookie, plaidItemID) {
  return fetch(`${acceptanceAPIURL()}/plaid/items/${encodeURIComponent(plaidItemID)}/sync-transactions`, {
    method: 'POST',
    headers: { cookie },
  })
}

// createUpdateLinkToken requests a Plaid Link "update mode" token scoped to
// plaidItemID — the #364 reconnect flow. Browser automation must not drive
// the Plaid Link iframe (see docs/QA.md), so this only proves the API seam
// that the Settings "Reconnect" CTA calls, not the full Link round trip.
export async function createUpdateLinkToken(cookie, plaidItemID) {
  const res = await fetch(`${acceptanceAPIURL()}/plaid/link/token`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', cookie },
    body: JSON.stringify({ plaid_item_id: plaidItemID }),
  })
  if (!res.ok) throw new Error(`Offbook update-mode link token failed: HTTP ${res.status} ${await res.text()}`)
  return res.json()
}
