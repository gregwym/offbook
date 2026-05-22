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
