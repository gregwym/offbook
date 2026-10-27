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
              last_sync_error: 'ITEM_LOGIN_REQUIRED: the login details of this item have changed',
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
    expect(screen.getByText(/needs you to sign in again/)).toBeInTheDocument()
    // #641: the raw Plaid code tells the user nothing they can act on.
    expect(screen.queryByText(/ITEM_LOGIN_REQUIRED/)).not.toBeInTheDocument()
    expect(document.getElementById('linked-institutions')).not.toBeNull()
    await expectHealthySmoke()
  })

  // #641: a failed Reconnect used to clear its own message (the error was
  // tied to the in-flight item id, which the failure resets).
  it('keeps a failed Reconnect message on the row', async () => {
    server.use(
      http.get(`${API}/plaid/items`, () =>
        HttpResponse.json({
          data: [
            {
              id: 1, user_id: 1, plaid_item_id: 'item-reauth-1', institution_name: 'Test Bank', status: 'active',
              last_sync_status: 'reauth_required', last_sync_error: null, unresolved_sync_errors: 0,
              created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
            },
          ],
          total: 1,
        }),
      ),
      http.post(`${API}/plaid/link/token`, () =>
        HttpResponse.json({ error: 'plaid is not configured on this instance', code: 'PLAID_NOT_CONFIGURED' }, { status: 503 }),
      ),
    )
    renderPage(<SettingsPage />)

    fireEvent.click(await screen.findByRole('button', { name: 'Reconnect Test Bank' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t start the reconnect: Plaid is not configured on this instance.',
    )
    expect(screen.getByRole('button', { name: 'Reconnect Test Bank' })).toHaveTextContent('Reconnect')
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

  // #575: delete or merge your own category, with a required destination.
  describe('delete or merge', () => {
    const mk = (id: number, name: string) => ({
      id, user_id: 1, parent_id: null, name, slug: name.toLowerCase(), is_system: false,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    })
    const coffee = mk(98, 'Coffee')
    const cafes = mk(99, 'Cafes')
    const usage = { transactions: 3, splits: 1, rules: 1, budgets: 1, subcategories: 0 }

    it('requires a destination, then merges into the chosen category', async () => {
      let deleted: string | null = null
      let list = [coffee, cafes]
      server.use(
        http.get(`${API}/categories`, () => HttpResponse.json({ data: list, total: list.length })),
        http.get(`${API}/categories/98/usage`, () => HttpResponse.json({ data: usage })),
        http.delete(`${API}/categories/98`, ({ request }) => {
          deleted = new URL(request.url).searchParams.get('replace_with')
          list = [cafes]
          return HttpResponse.json({ data: usage })
        }),
      )
      renderPage(<SettingsPage />)

      fireEvent.click(await screen.findByRole('button', { name: 'Delete or merge Coffee' }))
      expect(await screen.findByText('Used by 3 transactions, 1 split line, 1 rule and 1 budget.')).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Delete “Coffee”' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('Choose where to move what uses this category.')
      expect(deleted).toBeNull()

      fireEvent.change(screen.getByLabelText('Move everything to'), { target: { value: '99' } })
      expect(screen.getByText(/moves to “Cafes”, which merges the two/)).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: 'Merge into “Cafes”' }))

      await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Merged “Coffee” into “Cafes”'))
      expect(deleted).toBe('99')
      expect(useCategoriesStore.getState().categories.map((c) => c.name)).toEqual(['Cafes'])
      expect(screen.queryByRole('button', { name: 'Delete or merge Coffee' })).not.toBeInTheDocument()
      await expectHealthySmoke()
    })

    it('deletes to Uncategorized and explains what happens to rules and budgets', async () => {
      let deleted: string | null = null
      server.use(
        http.get(`${API}/categories`, () => HttpResponse.json({ data: [coffee], total: 1 })),
        http.get(`${API}/categories/98/usage`, () => HttpResponse.json({ data: usage })),
        http.delete(`${API}/categories/98`, ({ request }) => {
          deleted = new URL(request.url).searchParams.get('replace_with')
          return HttpResponse.json({ data: usage })
        }),
      )
      renderPage(<SettingsPage />)

      fireEvent.click(await screen.findByRole('button', { name: 'Delete or merge Coffee' }))
      fireEvent.change(await screen.findByLabelText('Move everything to'), { target: { value: 'none' } })
      expect(screen.getByText(/become uncategorized/)).toHaveTextContent('Budgets for “Coffee” are removed.')
      fireEvent.click(screen.getByRole('button', { name: 'Delete “Coffee”' }))

      await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Deleted “Coffee” and moved 4 transactions'))
      expect(deleted).toBe('none')
    })

    it('keeps the panel open with a sentence when the delete fails', async () => {
      server.use(
        http.get(`${API}/categories`, () => HttpResponse.json({ data: [coffee, cafes], total: 2 })),
        http.get(`${API}/categories/98/usage`, () => HttpResponse.json({ data: usage })),
        http.delete(`${API}/categories/98`, () =>
          HttpResponse.json({ error: 'pick another', code: 'INVALID_REPLACEMENT' }, { status: 400 })),
      )
      renderPage(<SettingsPage />)

      fireEvent.click(await screen.findByRole('button', { name: 'Delete or merge Coffee' }))
      fireEvent.change(await screen.findByLabelText('Move everything to'), { target: { value: '99' } })
      fireEvent.click(screen.getByRole('button', { name: 'Merge into “Cafes”' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('That category is no longer available. Pick another one.')
      expect(screen.getByRole('button', { name: 'Merge into “Cafes”' })).toBeEnabled()
    })
  })
})
