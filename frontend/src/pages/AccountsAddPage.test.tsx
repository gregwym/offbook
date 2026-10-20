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

  // #520: the manual form speaks plain language and Enter submits it.
  it('manual form uses plain labels, hints liability sign, and submits on Enter', async () => {
    let body: Record<string, unknown> | null = null
    server.use(
      http.post(`${API}/accounts`, async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ data: { id: 9, name: body.name } }, { status: 201 })
      }),
    )
    renderPage(<AccountsAddPage />)
    fireEvent.click(screen.getByRole('button', { name: /^add manually$/i }))

    expect(screen.queryByText(/slug/i)).toBeNull()
    expect(screen.getByRole('option', { name: 'Credit card' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'credit_card' } })
    expect(screen.getByText(/what you owe as a negative number/i)).toBeTruthy()

    fireEvent.change(screen.getByLabelText('Account name'), { target: { value: 'Visa' } })
    fireEvent.change(screen.getByLabelText('Institution'), { target: { value: 'Chase' } })
    fireEvent.change(screen.getByLabelText('Current balance (optional)'), { target: { value: '-1,250.50' } })
    fireEvent.submit(screen.getByLabelText('Account name').closest('form')!)

    await waitFor(() => expect(body).not.toBeNull())
    expect(body).toMatchObject({ name: 'Visa', institution_slug: 'Chase', account_type: 'credit_card', balance: '-1250.50' })
  })

  it('rejects a non-numeric balance with a clear message', async () => {
    renderPage(<AccountsAddPage />)
    fireEvent.click(screen.getByRole('button', { name: /^add manually$/i }))
    fireEvent.change(screen.getByLabelText('Account name'), { target: { value: 'Cash' } })
    fireEvent.change(screen.getByLabelText('Institution'), { target: { value: 'Wallet' } })
    fireEvent.change(screen.getByLabelText('Current balance (optional)'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }))
    expect((await screen.findByRole('alert')).textContent).toMatch(/as a number/i)
  })
})
