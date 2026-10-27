// #641: a bank in reauth_required must be visible from every personal page,
// not only Settings, with a path to the Reconnect action.
import { screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { server } from '../test/server'
import { renderPage, resetStores } from '../test/testUtils'
import type { PlaidItem } from '../types/plaid'
import { ReconnectBanner } from './ReconnectBanner'

const API = '/api/v1'

function item(id: number, name: string, status: PlaidItem['last_sync_status']): PlaidItem {
  return {
    id,
    user_id: 1,
    plaid_item_id: `item-${id}`,
    institution_name: name,
    status: 'active',
    last_sync_status: status,
    last_synced_at: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
    last_sync_error: 'ITEM_LOGIN_REQUIRED: the login details of this item have changed',
    unresolved_sync_errors: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function serveItems(items: PlaidItem[]) {
  let calls = 0
  server.use(
    http.get(`${API}/plaid/items`, () => {
      calls++
      return HttpResponse.json({ data: items, total: items.length })
    }),
  )
  return () => calls
}

describe('ReconnectBanner', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders nothing when every linked bank is healthy', async () => {
    const calls = serveItems([item(1, 'Chase', 'ok')])
    const { container } = renderPage(<ReconnectBanner />, '/insights')
    await waitFor(() => expect(calls()).toBe(1))
    expect(container).toBeEmptyDOMElement()
  })

  it('names the one bank that needs reconnecting and links to Settings', async () => {
    serveItems([item(1, 'Chase', 'reauth_required'), item(2, 'Ally', 'ok')])
    renderPage(<ReconnectBanner />, '/insights')

    expect(await screen.findByText('Chase needs you to sign in again')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Last synced 3h ago. Balances and transactions from it stop updating until you reconnect.',
    )
    expect(screen.getByRole('link', { name: 'Reconnect' })).toHaveAttribute('href', '/settings#linked-institutions')
    expect(screen.queryByText(/ITEM_LOGIN_REQUIRED/)).not.toBeInTheDocument()
  })

  it('counts several banks instead of naming one', async () => {
    serveItems([item(1, 'Chase', 'reauth_required'), item(2, 'Ally', 'reauth_required')])
    renderPage(<ReconnectBanner />, '/transactions')

    expect(await screen.findByText('2 banks need you to sign in again')).toBeInTheDocument()
    expect(screen.queryByText(/Last synced/)).not.toBeInTheDocument()
  })

  it('stays out of Settings, which hosts the Reconnect button itself', () => {
    const calls = serveItems([item(1, 'Chase', 'reauth_required')])
    const { container } = renderPage(<ReconnectBanner />, '/settings')
    expect(container).toBeEmptyDOMElement()
    expect(calls()).toBe(0)
  })

  it('renders nothing when the items request fails', async () => {
    let calls = 0
    server.use(
      http.get(`${API}/plaid/items`, () => {
        calls++
        return HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 })
      }),
    )
    const { container } = renderPage(<ReconnectBanner />, '/insights')
    await waitFor(() => expect(calls).toBe(1))
    expect(container).toBeEmptyDOMElement()
  })
})
