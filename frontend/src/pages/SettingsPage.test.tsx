import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SettingsPage } from './SettingsPage'

const API = '/api/v1'

describe('SettingsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders AI, price, and linked-institution sections without a stuck loading state or error banner', async () => {
    renderPage(<SettingsPage />)
    await expectHealthySmoke()
  })

  // #364: a plaid_item stuck in reauth_required is an expected, actionable
  // state — not a page error — so it must render a "Reconnect" CTA without
  // tripping the shared error-banner smoke check.
  it('shows a Reconnect CTA for a reauth_required item, not an error banner', async () => {
    server.use(
      http.get(`${API}/plaid/items`, () =>
        HttpResponse.json({
          data: [
            {
              id: 1,
              user_id: 1,
              plaid_item_id: 'item-reauth-1',
              institution_name: 'Test Bank',
              status: 'active',
              last_sync_status: 'reauth_required',
              last_sync_error: 'the login details of this item have changed',
              unresolved_sync_errors: 0,
              created_at: '2026-01-01T00:00:00Z',
              updated_at: '2026-01-01T00:00:00Z',
            },
          ],
          total: 1,
        }),
      ),
    )

    renderPage(<SettingsPage />)

    expect(await screen.findByRole('button', { name: 'Reconnect Test Bank' })).toBeInTheDocument()
    expect(screen.getByText(/needs you to reconnect/)).toBeInTheDocument()
    await expectHealthySmoke()
  })
})
