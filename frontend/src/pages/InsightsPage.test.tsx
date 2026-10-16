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
