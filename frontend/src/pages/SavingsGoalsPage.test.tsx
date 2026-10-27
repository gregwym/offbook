import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { SavingsGoalsPage } from './SavingsGoalsPage'

describe('SavingsGoalsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the personal-scope goal list without a stuck loading state or error banner', async () => {
    renderPage(<SavingsGoalsPage />)
    await expectHealthySmoke()
  })

  it('offers a create button in the empty state that opens the goal form (#625)', async () => {
    server.use(http.get('*/savings-goals', () => HttpResponse.json({ data: [], total: 0 })))
    renderPage(<SavingsGoalsPage />)
    fireEvent.click(await screen.findByRole('button', { name: 'Create your first goal' }))
    expect(await screen.findByRole('dialog')).toBeTruthy()
  })
})
