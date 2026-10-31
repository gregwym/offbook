import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, screen, within } from '@testing-library/react'
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

  it('names each goal card action after its goal (#686)', async () => {
    renderPage(<SavingsGoalsPage />)
    expect(await screen.findByRole('button', { name: 'Edit Emergency fund goal' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Delete Emergency fund goal' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull()
  })

  it('opens a goal\'s history with dates, signed amounts and running totals (#682)', async () => {
    server.use(
      http.get('*/savings-goals/1/contributions', () =>
        HttpResponse.json({
          data: [
            { id: 3, goal_id: 1, amount: '-25.000000000000000000', balance_after: '200.000000000000000000', created_at: '2026-10-29T15:00:00Z' },
            { id: 2, goal_id: 1, amount: '125.000000000000000000', balance_after: '225.000000000000000000', created_at: '2026-10-20T15:00:00Z' },
          ],
          total: 2,
        }),
      ),
    )
    renderPage(<SavingsGoalsPage />)
    const toggle = await screen.findByRole('button', { name: /^History/ })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(toggle)
    const list = await screen.findByRole('list', { name: 'Contribution history for Emergency fund' })
    const items = within(list).getAllByRole('listitem')
    expect(items).toHaveLength(3)
    expect(items[0].textContent).toContain('Withdrawal')
    expect(items[0].textContent).toContain('−25.00')
    expect(items[0].textContent).toContain('Total 200.00')
    expect(items[1].textContent).toContain('Contribution')
    expect(items[1].textContent).toContain('+125.00')
    expect(items[2].textContent).toContain('Saved before history was kept')
    expect(items[2].textContent).toContain('Total 100.00')
    expect(screen.getByRole('button', { name: /Hide history/ }).getAttribute('aria-expanded')).toBe('true')
  })

  it('says so when a goal has no contributions yet, and offers a retry when history fails to load (#682)', async () => {
    let fail = true
    server.use(
      http.get('*/savings-goals', () =>
        HttpResponse.json({ data: [{ ...goalZero }], total: 1 }),
      ),
      http.get('*/savings-goals/1/contributions', () =>
        fail ? HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 }) : HttpResponse.json({ data: [], total: 0 }),
      ),
    )
    renderPage(<SavingsGoalsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /^History/ }))
    expect(await screen.findByText('Couldn’t load the history.')).toBeTruthy()
    fail = false
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText(/No contributions yet/)).toBeTruthy()
  })
})

const goalZero = {
  id: 1,
  user_id: 1,
  household_id: null,
  name: 'New car',
  target_amount: '1000.000000000000000000',
  current_amount: '0.000000000000000000',
  target_date: null,
  account_id: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  progress_pct: 0,
  remaining: '1000.000000000000000000',
}
