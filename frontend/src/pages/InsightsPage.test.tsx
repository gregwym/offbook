import { beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { server } from '../test/server'
import { expectHealthySmoke, renderPage, resetStores } from '../test/testUtils'
import { InsightsPage } from './InsightsPage'

describe('InsightsPage smoke', () => {
  beforeEach(() => {
    resetStores()
  })

  it('renders the 5 personal-scope bands without a stuck loading state or error banner', async () => {
    renderPage(<InsightsPage />)
    await expectHealthySmoke()
  })
})

describe('InsightsPage net-worth range selector', () => {
  beforeEach(() => {
    resetStores()
  })

  it('refetches only the trend with the chosen months and marks it pressed', async () => {
    const requested: string[] = []
    server.use(
      http.get('*/dashboard/net-worth', ({ request }) => {
        requested.push(new URL(request.url).searchParams.get('months') ?? '')
        return HttpResponse.json({ data: [], total: 0 })
      }),
    )
    renderPage(<InsightsPage />)
    const group = await screen.findByRole('group', { name: 'Net worth date range' })
    expect(screen.getByRole('button', { name: '1Y' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(screen.getByRole('button', { name: '3M' }))
    await waitFor(() => expect(requested).toContain('3'))
    expect(screen.getByRole('button', { name: '3M' })).toHaveAttribute('aria-pressed', 'true')
    expect(group).toBeInTheDocument()
  })

  it('keeps the previous series and shows an error when the range fetch fails', async () => {
    renderPage(<InsightsPage />)
    await screen.findByRole('group', { name: 'Net worth date range' })
    server.use(
      http.get('*/dashboard/net-worth', () =>
        HttpResponse.json({ error: 'boom', code: 'INTERNAL' }, { status: 500 }),
      ),
    )
    fireEvent.click(screen.getByRole('button', { name: '6M' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(/Couldn.t load that range/)
    expect(screen.getByRole('button', { name: '1Y' })).toHaveAttribute('aria-pressed', 'true')
  })
})

describe('InsightsPage net-worth asset-class drill-in', () => {
  beforeEach(() => {
    resetStores()
  })

  it('shows start vs latest value per asset class when by_kind is present', async () => {
    server.use(
      http.get('*/dashboard/net-worth', () =>
        HttpResponse.json({
          data: [
            { date: '2026-03-31', total: '1000', complete: true, by_kind: [{ kind: 'fiat', value: '1000', complete: true }] },
            {
              date: '2026-05-31',
              total: '1650',
              complete: true,
              by_kind: [
                { kind: 'equity', value: '150', complete: true },
                { kind: 'fiat', value: '1500', complete: false },
              ],
            },
          ],
          total: 2,
        }),
      ),
    )
    renderPage(<InsightsPage />)
    fireEvent.click(await screen.findByText('By asset class'))
    const row = screen.getByRole('rowheader', { name: /equity/i }).closest('tr') as HTMLElement
    expect(row).toHaveTextContent('150.00')
    expect(screen.getByRole('rowheader', { name: /fiat/i }).closest('tr')).toHaveTextContent('500.00')
  })
})
