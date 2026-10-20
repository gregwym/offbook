import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'
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
})
