import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { useCategoriesStore } from '../store/categoriesStore'
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

  // #366: the AI categorization opt-in persists via PATCH /me/settings, same
  // shape as the existing price-refresh toggle.
  it('toggles AI auto-categorization on', async () => {
    server.use(
      http.patch(`${API}/me/settings`, () =>
        HttpResponse.json({
          data: {
            user_id: 1,
            preferred_provider: 'claude',
            api_endpoint: null,
            api_token_set: false,
            preferred_model: null,
            auto_price_refresh: false,
            auto_categorize: true,
          },
        }),
      ),
    )

    renderPage(<SettingsPage />)
    await expectHealthySmoke()

    const toggle = await screen.findByRole('checkbox', { name: /categorize with ai automatically/i })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)

    await waitFor(() => expect(toggle).toBeChecked())
  })
})

// #573: a user adds a category (optionally grouped), sees it listed and
// shared with every picker, renames it, and sees duplicate errors inline.
describe('SettingsPage categories', () => {
  beforeEach(() => {
    resetStores()
  })

  it('adds a category under a group, then renames it', async () => {
    const created = {
      id: 99, user_id: 1, parent_id: 1, name: 'Coffee', slug: 'coffee', is_system: false,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }
    let posted: unknown = null
    server.use(
      http.post(`${API}/categories`, async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({ data: created }, { status: 201 })
      }),
      http.patch(`${API}/categories/99`, async ({ request }) => {
        const body = (await request.json()) as { name: string }
        return HttpResponse.json({ data: { ...created, name: body.name } })
      }),
    )
    renderPage(<SettingsPage />)

    expect(await screen.findByText(/You haven't added any yet/)).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('New category'), { target: { value: 'Coffee' } })
    fireEvent.change(screen.getByLabelText('Group under (optional)'), { target: { value: '1' } })
    fireEvent.click(screen.getByRole('button', { name: /Add category/ }))

    expect(await screen.findByText('in Groceries')).toBeInTheDocument()
    expect(posted).toEqual({ name: 'Coffee', parent_id: 1 })
    expect(useCategoriesStore.getState().categories.map((c) => c.name)).toContain('Coffee')

    fireEvent.click(screen.getByRole('button', { name: 'Rename Coffee' }))
    fireEvent.change(screen.getByLabelText('New name for Coffee'), { target: { value: 'Coffee shops' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Coffee shops')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Renamed to “Coffee shops”')
    await expectHealthySmoke()
  })

  it('shows a duplicate name inline and keeps the typed name', async () => {
    server.use(
      http.post(`${API}/categories`, () =>
        HttpResponse.json(
          { error: 'a category with this name already exists', code: 'CATEGORY_NAME_TAKEN' },
          { status: 409 },
        ),
      ),
    )
    renderPage(<SettingsPage />)

    await screen.findByText(/You haven't added any yet/)
    fireEvent.change(screen.getByLabelText('New category'), { target: { value: 'groceries' } })
    fireEvent.click(screen.getByRole('button', { name: /Add category/ }))

    expect(await screen.findByRole('alert')).toHaveTextContent('“groceries” already exists. Pick a different name.')
    expect(screen.getByLabelText('New category')).toHaveValue('groceries')
  })

  it('asks for a name instead of posting a blank one', async () => {
    renderPage(<SettingsPage />)
    await screen.findByText(/You haven't added any yet/)
    fireEvent.click(screen.getByRole('button', { name: /Add category/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter a name for the category.')
  })
})
