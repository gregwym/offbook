import { beforeEach, describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { server } from '../test/server'
import { AccountsAddPage } from './AccountsAddPage'

// Simulate Plaid Link completing instantly: open() reports success.
vi.mock('react-plaid-link', () => ({
  usePlaidLink: ({ onSuccess }: { onSuccess: (t: string) => void }) => ({
    ready: true,
    open: () => onSuccess('public-sandbox-token'),
  }),
}))

const API = '/api/v1'

describe('AccountsAddPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the two-tile picker without a stuck loading state or error banner', async () => {
    renderPage(<AccountsAddPage />)
    await expectHealthySmoke()
  })

  it('resumes the sync for the linked item instead of re-linking when sync fails', async () => {
    let tokenCalls = 0
    let exchangeCalls = 0
    let syncAttempts = 0
    server.use(
      http.post(`${API}/plaid/link/token`, () => {
        tokenCalls++
        return HttpResponse.json({ data: { link_token: 'link-sandbox-1' } })
      }),
      http.post(`${API}/plaid/link/exchange`, () => {
        exchangeCalls++
        return HttpResponse.json({ data: { item_id: 'item-1' } })
      }),
      http.post(`${API}/plaid/items/item-1/sync-accounts`, () => {
        syncAttempts++
        return syncAttempts === 1
          ? HttpResponse.json({ error: 'Plaid is slow', code: 'PLAID_ERROR' }, { status: 502 })
          : HttpResponse.json({ data: {} })
      }),
      http.post(`${API}/plaid/items/item-1/sync-transactions`, () =>
        HttpResponse.json({ data: {} }),
      ),
    )
    renderPage(<AccountsAddPage />)
    fireEvent.click(screen.getByRole('button', { name: /connect a bank/i }))

    expect(await screen.findByText(/bank connected, but the import didn’t finish/i)).toBeTruthy()
    expect(screen.getByRole('link', { name: /go to accounts/i })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: /retry import/i }))
    await waitFor(() => expect(syncAttempts).toBe(2))
    expect(tokenCalls).toBe(1)
    expect(exchangeCalls).toBe(1)
  })
})
