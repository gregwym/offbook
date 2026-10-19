import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { renderPage, resetStores } from '../test/testUtils'
import { server } from '../test/server'
import { PLAID_OAUTH_STORAGE_KEY, loadPendingLink, savePendingLink } from '../lib/plaidOAuth'
import { PlaidOAuthReturnPage } from './PlaidOAuthReturnPage'

// Controls what the mocked Plaid Link does on open(): finish with a
// public_token, or exit with/without an error. Captures the config the page
// passed so we can assert token + receivedRedirectUri.
type LinkBehavior = { kind: 'success' } | { kind: 'exit'; error: Record<string, string> | null }
const link = vi.hoisted(() => ({
  behavior: { kind: 'success' } as LinkBehavior,
  configs: [] as Array<{ token: string | null; receivedRedirectUri?: string }>,
  opens: 0,
}))

vi.mock('react-plaid-link', () => ({
  usePlaidLink: (cfg: {
    token: string | null
    receivedRedirectUri?: string
    onSuccess: (t: string) => void
    onExit: (e: unknown) => void
  }) => {
    link.configs.push({ token: cfg.token, receivedRedirectUri: cfg.receivedRedirectUri })
    return {
      ready: cfg.token !== null,
      open: () => {
        link.opens++
        if (link.behavior.kind === 'success') cfg.onSuccess('public-sandbox-oauth')
        else cfg.onExit(link.behavior.error)
      },
    }
  },
}))

const API = '/api/v1'
const future = () => new Date(Date.now() + 60 * 60 * 1000).toISOString()

function renderReturn() {
  return renderPage(
    <Routes>
      <Route path="/plaid/oauth-return" element={<PlaidOAuthReturnPage />} />
      <Route path="/accounts" element={<div>Accounts landing</div>} />
      <Route path="/settings" element={<div>Settings landing</div>} />
      <Route path="/accounts/add" element={<div>Add account landing</div>} />
    </Routes>,
    '/plaid/oauth-return?oauth_state_id=abc',
  )
}

describe('PlaidOAuthReturnPage', () => {
  beforeEach(() => {
    resetStores()
    window.sessionStorage.clear()
    link.behavior = { kind: 'success' }
    link.configs = []
    link.opens = 0
  })

  it('resumes Link with the stored token and finishes exchange → syncs for a new link', async () => {
    const calls: string[] = []
    server.use(
      http.post(`${API}/plaid/link/exchange`, async ({ request }) => {
        const body = (await request.json()) as { public_token: string }
        calls.push(`exchange:${body.public_token}`)
        return HttpResponse.json({ data: { item_id: 'item-oauth' } })
      }),
      http.post(`${API}/plaid/items/item-oauth/sync-accounts`, () => {
        calls.push('sync-accounts')
        return HttpResponse.json({ data: {} })
      }),
      http.post(`${API}/plaid/items/item-oauth/sync-transactions`, () => {
        calls.push('sync-transactions')
        return HttpResponse.json({ data: {} })
      }),
    )
    savePendingLink({ mode: 'new', linkToken: 'link-production-1', expiration: future() })

    renderReturn()

    expect(await screen.findByText('Accounts landing')).toBeTruthy()
    expect(calls).toEqual(['exchange:public-sandbox-oauth', 'sync-accounts', 'sync-transactions'])
    const opened = link.configs.find((c) => c.token === 'link-production-1')
    expect(opened?.receivedRedirectUri).toBe(window.location.href)
    expect(link.opens).toBe(1)
    expect(window.sessionStorage.getItem(PLAID_OAUTH_STORAGE_KEY)).toBeNull()
  })

  it('re-auth (update mode) skips the exchange and returns to Settings', async () => {
    let exchanged = false
    const synced: string[] = []
    server.use(
      http.post(`${API}/plaid/link/exchange`, () => {
        exchanged = true
        return HttpResponse.json({ data: { item_id: 'x' } })
      }),
      http.post(`${API}/plaid/items/item-7/sync-accounts`, () => {
        synced.push('accounts')
        return HttpResponse.json({ data: {} })
      }),
      http.post(`${API}/plaid/items/item-7/sync-transactions`, () => {
        synced.push('transactions')
        return HttpResponse.json({ data: {} })
      }),
    )
    savePendingLink({ mode: 'update', linkToken: 'link-update-1', expiration: future(), itemId: 'item-7' })

    renderReturn()

    expect(await screen.findByText('Settings landing')).toBeTruthy()
    expect(exchanged).toBe(false)
    expect(synced).toEqual(['accounts', 'transactions'])
  })

  it('shows a retry path when no stored link session exists', async () => {
    renderReturn()

    expect(await screen.findByRole('alert')).toHaveProperty(
      'textContent',
      'We couldn’t find a bank sign-in to finish',
    )
    expect(link.opens).toBe(0)
    fireEvent.click(screen.getByRole('link', { name: /back to add account/i }))
    expect(await screen.findByText('Add account landing')).toBeTruthy()
  })

  it('shows an expired message without opening Link when the stored token has expired', async () => {
    savePendingLink({
      mode: 'new',
      linkToken: 'link-old',
      expiration: new Date(Date.now() - 1000).toISOString(),
    })

    renderReturn()

    expect((await screen.findByRole('alert')).textContent).toBe('Your bank sign-in session expired')
    expect(link.opens).toBe(0)
    expect(screen.getByRole('link', { name: /back to add account/i })).toBeTruthy()
  })

  it('treats a Plaid INVALID_LINK_TOKEN exit as expired and clears the stored session', async () => {
    link.behavior = {
      kind: 'exit',
      error: { error_code: 'INVALID_LINK_TOKEN', error_type: 'INVALID_INPUT', error_message: 'x', display_message: '' },
    }
    savePendingLink({ mode: 'update', linkToken: 'link-bad', expiration: future(), itemId: 'item-7' })

    renderReturn()

    expect((await screen.findByRole('alert')).textContent).toBe('Your bank sign-in session expired')
    expect(screen.getByRole('link', { name: /back to settings/i })).toBeTruthy()
    expect(loadPendingLink()).toBeNull()
  })

  it('shows a cancel message when the user closes Link', async () => {
    link.behavior = { kind: 'exit', error: null }
    savePendingLink({ mode: 'new', linkToken: 'link-1', expiration: future() })

    renderReturn()

    expect((await screen.findByRole('alert')).textContent).toBe('Bank sign-in didn’t finish')
    expect(screen.getByRole('link', { name: /back to add account/i })).toBeTruthy()
  })

  it('resumes syncs after a reload instead of exchanging twice, with a retry on failure', async () => {
    let exchanges = 0
    let attempts = 0
    server.use(
      http.post(`${API}/plaid/link/exchange`, () => {
        exchanges++
        return HttpResponse.json({ data: { item_id: 'item-r' } })
      }),
      http.post(`${API}/plaid/items/item-r/sync-accounts`, () => {
        attempts++
        return attempts === 1
          ? HttpResponse.json({ error: 'Plaid is slow', code: 'PLAID_ERROR' }, { status: 502 })
          : HttpResponse.json({ data: {} })
      }),
      http.post(`${API}/plaid/items/item-r/sync-transactions`, () => HttpResponse.json({ data: {} })),
    )
    // State as left by a reload after a successful exchange.
    savePendingLink({ mode: 'new', linkToken: null, expiration: future(), itemId: 'item-r' })

    renderReturn()

    expect((await screen.findByRole('alert')).textContent).toBe('Bank connected, but the import didn’t finish')
    fireEvent.click(screen.getByRole('button', { name: /retry import/i }))
    expect(await screen.findByText('Accounts landing')).toBeTruthy()
    await waitFor(() => expect(attempts).toBe(2))
    expect(exchanges).toBe(0)
    expect(link.opens).toBe(0)
  })
})
