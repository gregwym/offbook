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

  // #655: grouped by kind with exact subtotals and a net-worth summary;
  // institution in words; inactive called out instead of an Active column.
  it('groups accounts with subtotals, a summary and readable labels', async () => {
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({
          data: [
            { ...fx.account1, id: 1, name: 'Everyday', balance: '20372.30' },
            { ...fx.account1, id: 2, name: 'Rainy day', account_type: 'savings', balance: '2750.00', is_active: false },
            { ...fx.account1, id: 3, name: 'Visa', account_type: 'credit_card', balance: '-12341.70', institution_slug: 'ins_3', plaid_item_id: 'item-1' },
            { ...fx.account1, id: 4, name: 'Brokerage', account_type: 'investment', balance: '1200.00' },
          ],
          total: 4,
        })),
      http.get('/api/v1/plaid/items', () =>
        HttpResponse.json({ data: [{ id: 1, user_id: 1, plaid_item_id: 'item-1', institution_name: 'Chase', status: 'active', last_sync_status: 'ok', created_at: '', updated_at: '' }], total: 1 })),
    )
    renderPage(<AccountsPage />)
    const summary = await screen.findByLabelText('Accounts summary')
    expect(summary).toHaveTextContent('Assets24,322.30 USD')
    expect(summary).toHaveTextContent('Liabilities-12,341.70 USD')
    expect(summary).toHaveTextContent('Net worth11,980.60 USD')
    expect(screen.getByLabelText('Cash total')).toHaveTextContent('23,122.30 USD')
    expect(screen.getByLabelText('Credit cards total')).toHaveTextContent('-12,341.70 USD')
    expect(screen.getByLabelText('Investments total')).toHaveTextContent('1,200.00 USD')
    expect(within(screen.getByRole('rowgroup', { name: 'Cash' })).getByText('Rainy day')).toBeTruthy()
    expect(screen.getByText('Inactive')).toBeTruthy()
    expect(await screen.findAllByText('Chase')).not.toHaveLength(0)
    expect(screen.queryByText('ins_3')).toBeNull()
    expect(screen.queryByText('manual')).toBeNull()
    expect(screen.queryByRole('columnheader', { name: 'Active' })).toBeNull()
  })

  it('points a mixed-currency book to Insights instead of adding currencies', async () => {
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({
          data: [{ ...fx.account1, id: 1 }, { ...fx.account1, id: 2, name: 'Euro', currency: 'EUR', balance: '5' }],
          total: 2,
        })),
    )
    renderPage(<AccountsPage />)
    const link = await screen.findByRole('link', { name: /net worth in one currency on insights/i })
    expect(link).toHaveAttribute('href', '/insights')
    expect(screen.queryByLabelText('Accounts summary')).toBeNull()
    expect(screen.getByLabelText('Cash total')).toHaveTextContent('1,234.56 USD')
    expect(screen.getByLabelText('Cash total')).toHaveTextContent('5.00 EUR')
  })

  // #657: the account name opens its transactions; Delete sits behind Edit,
  // never inline; columns nobody fills are dropped.
  it('links each account to its transactions and keeps Delete behind Edit', async () => {
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({
          data: [
            { ...fx.account1, id: 1, name: 'Everyday', last_four: null },
            { ...fx.account1, id: 3, name: 'QAM Card', account_type: 'credit_card', balance: '-12341.70', last_four: null },
          ],
          total: 2,
        })),
    )
    renderPage(<AccountsPage />)
    const link = await screen.findByRole('link', { name: 'QAM Card' })
    expect(link).toHaveAttribute('href', '/transactions?account_id=3')
    expect(screen.getByRole('link', { name: 'Everyday' })).toHaveAttribute('href', '/transactions?account_id=1')

    expect(screen.queryByRole('button', { name: /delete/i })).toBeNull()
    expect(screen.queryByRole('columnheader', { name: 'Last 4' })).toBeNull()
    expect(screen.queryByRole('columnheader', { name: 'Sync' })).toBeNull()
    // Every row action names its account.
    expect(screen.getAllByRole('button', { name: 'Edit QAM Card' }).length).toBeGreaterThan(0)

    let deleted = false
    server.use(http.delete('/api/v1/accounts/:id', () => { deleted = true; return new HttpResponse(null, { status: 204 }) }))
    const confirm = window.confirm
    window.confirm = () => true
    try {
      fireEvent.click(screen.getAllByRole('button', { name: 'Edit QAM Card' })[0])
      const dialog = await screen.findByRole('dialog')
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete account' }))
      await waitFor(() => expect(deleted).toBe(true))
      await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    } finally {
      window.confirm = confirm
    }
  })

  it('keeps Last 4 and Sync when some account has them', async () => {
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({
          data: [{ ...fx.account1, plaid_item_id: 'item-1', last_sync_status: 'ok', last_synced_at: new Date().toISOString() }],
          total: 1,
        })),
    )
    renderPage(<AccountsPage />)
    expect(await screen.findByRole('columnheader', { name: 'Last 4' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Sync' })).toBeTruthy()
  })
})
