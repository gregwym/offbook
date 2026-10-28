import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
import { AccountsPage } from '../pages/AccountsPage'
import { account1 } from '../test/fixtures'
import { server } from '../test/server'
import { renderPage, resetStores } from '../test/testUtils'

describe('Update balance (#504)', () => {
  beforeEach(() => {
    resetStores()
  })

  it('prefills the balance, validates, saves and explains the adjustment', async () => {
    let posted: unknown = null
    server.use(
      http.post('/api/v1/accounts/:id/balance', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({
          data: {
            account: { ...account1, balance: '1300.000000000000000000' },
            recorded: { id: 9, kind: 'adjustment', amount: '65.440000000000000000' },
          },
        })
      }),
    )
    renderPage(<AccountsPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Update balance for Checking' }))

    const input = screen.getByLabelText('Balance (USD)')
    expect(input).toHaveValue('1234.56')
    fireEvent.change(input, { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter the balance as a number')
    expect(posted).toBeNull()

    fireEvent.change(input, { target: { value: '1,300.00' } })
    // #512: the error clears as soon as the field holds a valid number.
    expect(screen.queryByRole('alert')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('1,300.00')
    expect(status).toHaveTextContent('an adjustment of')
    expect(status).toHaveTextContent('65.44')
    expect(posted).toEqual({ balance: '1300.00' })
    expect(screen.getByRole('link', { name: 'View reconciliation' })).toHaveAttribute('href', '/accounts/1/reconciliation')
  })

  // #669: a past balance is that day's; later transactions still count.
  it('saves a past-dated balance and shows today\'s balance separately', async () => {
    let posted: unknown = null
    server.use(
      http.post('/api/v1/accounts/:id/balance', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({
          data: { account: { ...account1, balance: '900.000000000000000000' }, recorded: null },
        })
      }),
    )
    renderPage(<AccountsPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Update balance for Checking' }))
    fireEvent.change(screen.getByLabelText('Balance (USD)'), { target: { value: '1000' } })
    fireEvent.change(screen.getByLabelText('As of'), { target: { value: '2026-05-31' } })
    expect(screen.getByLabelText('As of')).toHaveAccessibleDescription(/Transactions after that date stay as they are/)
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    const status = await screen.findByRole('status')
    expect(posted).toEqual({ balance: '1000', as_of: '2026-05-31' })
    expect(status).toHaveTextContent(/Saved 1,000\.00.*as your balance on May 31, 2026/)
    expect(status).toHaveTextContent(/Balance today is 900\.00/)
    expect(status).toHaveTextContent('Your transactions up to May 31, 2026 already add up to this balance')
  })

  // #667: a card's balance is the amount owed as the statement shows it.
  it('takes a credit card balance as the amount owed and stores it negative', async () => {
    let posted: unknown = null
    const card = { ...account1, id: 3, name: 'Visa', account_type: 'credit_card', balance: '-450.000000000000000000' }
    server.use(
      http.get('/api/v1/accounts', () => HttpResponse.json({ data: [card], total: 1 })),
      http.post('/api/v1/accounts/:id/balance', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({
          data: {
            account: { ...card, balance: '-512.300000000000000000' },
            recorded: { id: 9, kind: 'adjustment', amount: '-62.300000000000000000' },
          },
        })
      }),
    )
    renderPage(<AccountsPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Update balance for Visa' }))

    const input = screen.getByLabelText('Amount owed (USD)')
    expect(input).toHaveValue('450')
    expect(screen.getByText(/You owe/)).toHaveTextContent('You owe 450.00 USD.')
    fireEvent.change(input, { target: { value: '512.30' } })
    expect(screen.getByText(/You owe/)).toHaveTextContent('You owe 512.30 USD.')
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('You now owe 512.30 USD.')
    expect(posted).toEqual({ balance: '-512.3' })
  })

  it('shows the server error and keeps the form open', async () => {
    server.use(
      http.post('/api/v1/accounts/:id/balance', () =>
        HttpResponse.json({ error: "this account's balance comes from its bank connection", code: 'PLAID_MANAGED' }, { status: 409 }),
      ),
    )
    renderPage(<AccountsPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Update balance for Checking' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('bank connection'))
    expect(screen.getByRole('button', { name: 'Save balance' })).toBeEnabled()
  })

  it('is not offered on Plaid-linked accounts', async () => {
    server.use(
      http.get('/api/v1/accounts', () =>
        HttpResponse.json({ data: [{ ...account1, plaid_item_id: 'item-1', plaid_account_id: 'acc-1' }], total: 1 }),
      ),
    )
    renderPage(<AccountsPage />)
    await screen.findAllByText('Checking')
    expect(screen.queryByRole('button', { name: /update balance/i })).toBeNull()
  })
})
