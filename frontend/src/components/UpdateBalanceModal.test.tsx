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
    fireEvent.click(screen.getByRole('button', { name: 'Save balance' }))
    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('1,300.00')
    expect(status).toHaveTextContent('an adjustment of')
    expect(status).toHaveTextContent('65.44')
    expect(posted).toEqual({ balance: '1300.00' })
    expect(screen.getByRole('link', { name: 'View reconciliation' })).toHaveAttribute('href', '/accounts/1/reconciliation')
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
    await screen.findByText('Checking')
    expect(screen.queryByRole('button', { name: /update balance/i })).toBeNull()
  })
})
