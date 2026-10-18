import { screen } from '@testing-library/react'
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
})
