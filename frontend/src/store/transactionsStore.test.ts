// Regression for #458: an unfiltered fetch that resolves after the
// ?review=1-filtered fetch must not overwrite the filtered list.
import { beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse, delay } from 'msw'
import { server } from '../test/server'
import { resetStores } from '../test/testUtils'
import { useTransactionsStore } from './transactionsStore'
import * as fx from '../test/fixtures'

const API = '/api/v1'

describe('transactionsStore fetch ordering', () => {
  beforeEach(() => {
    resetStores()
    useTransactionsStore.setState({ filter: {}, page: 0, transactions: [], total: 0 })
  })

  it('ignores a slow superseded response', async () => {
    server.use(
      http.get(`${API}/transactions`, async ({ request }) => {
        const filtered = new URL(request.url).searchParams.get('reviewed') === 'false'
        if (!filtered) {
          await delay(150)
          return HttpResponse.json({ data: fx.transactions, total: fx.transactions.length })
        }
        return HttpResponse.json({ data: [], total: 0 })
      }),
    )
    const s = useTransactionsStore.getState()
    const slow = s.fetch() // mount fetch, default filter
    s.setFilter({ reviewed: false }) // URL-seeded filter, resolves first
    await slow
    await new Promise((r) => setTimeout(r, 200))
    const st = useTransactionsStore.getState()
    expect(st.transactions).toEqual([])
    expect(st.total).toBe(0)
    expect(st.loading).toBe(false)
  })
})
