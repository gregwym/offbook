import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import * as fx from '../test/fixtures'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { AccountsPage } from './AccountsPage'

describe('AccountsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders without a stuck loading state or error banner', async () => {
    renderPage(<AccountsPage />)
    await expectHealthySmoke()
  })

  // ONB-01: a brand-new user sees a next step, not a dead-end table row.
  it('offers an add-account call to action when there are no accounts', async () => {
    server.use(http.get('/api/v1/accounts', () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<AccountsPage />)
    const cta = await screen.findByRole('link', { name: /add your first account/i })
    expect(cta).toHaveAttribute('href', '/accounts/add')
  })

  // #520: right after a manual account is created, confirm it and offer
  // importing its transactions as the next step.
  it('confirms a just-created account and opens import preset to it', async () => {
    render(
      <MemoryRouter initialEntries={[{ pathname: '/accounts', state: { created: { id: 1, name: 'Checking' } } }]}>
        <AccountsPage />
      </MemoryRouter>,
    )
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('Checking added.')
    const importBtn = within(status).getByRole('button', { name: /^import transactions$/i })
    await waitFor(() => expect(importBtn).toBeEnabled())
    fireEvent.click(importBtn)
    expect(await screen.findByText('Import transactions', { selector: 'span' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy()

    // Dismissable.
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText('Checking added.')).toBeNull()
  })

  it('shows plain type labels instead of wire enums', async () => {
    renderPage(<AccountsPage />)
    expect((await screen.findAllByText('Checking')).length).toBeGreaterThan(0)
    expect(screen.queryByText('credit_card')).toBeNull()
  })

  // #530: a partial balance names the holding missing a price, and Set price
  // opens pre-filled with it so the user only types the price.
  it('names the unpriced holding and pre-fills Set price with it', async () => {
    const vti = { id: 7, symbol: 'VTI', kind: 'equity' as const, display_name: null, quote_currency_asset_id: 1 }
    let posted: { assetID: string; body: unknown } | null = null
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({
          data: [{
            ...fx.account1, id: 2, name: 'Brokerage', account_type: 'investment',
            balance: '50.000000000000000000', balance_complete: false,
            unpriced_assets: [{ asset_id: 7, symbol: 'VTI', kind: 'equity' }],
          }],
          total: 1,
        })),
      http.get('/api/v1/assets', () => HttpResponse.json({ data: [fx.assetUSD, vti], total: 2 })),
      http.post('/api/v1/assets/:id/prices', async ({ params, request }) => {
        posted = { assetID: String(params.id), body: await request.json() }
        return HttpResponse.json({ data: { id: 1, asset_id: 7, quote_asset_id: 1, price: '250', as_of: '2026-10-20', source: 'manual' } }, { status: 201 })
      }),
    )
    renderPage(<AccountsPage />)
    expect(await screen.findByText('Needs a price:')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Set price for VTI' }))

    const dialog = await screen.findByRole('dialog', { name: 'Set price for VTI' })
    expect(within(dialog).getByPlaceholderText('e.g. MYFUND')).toHaveValue('VTI')
    const priceInput = within(dialog).getByPlaceholderText('100.00')
    expect(priceInput).toHaveFocus()
    fireEvent.change(priceInput, { target: { value: '250' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Set price' }))
    await waitFor(() => expect(posted?.assetID).toBe('7'))
    expect(posted!.body).toMatchObject({ price: '250' })
  })
})
