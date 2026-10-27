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

// #633: an Undo still in flight must not land after the user's next review,
// or the server keeps the un-reviewed state the user just overrode.
describe('transactionsStore review write ordering', () => {
  beforeEach(() => {
    resetStores()
    useTransactionsStore.setState({ filter: { reviewed: false }, page: 0, transactions: [], total: 0 })
  })

  it('sends review writes one at a time in click order', async () => {
    const server_reviewed = new Map<number, boolean>()
    const started: boolean[] = []
    server.use(
      http.post(`${API}/transactions/review`, async ({ request }) => {
        const body = (await request.json()) as { ids: number[]; reviewed: boolean }
        started.push(body.reviewed)
        // The Undo (reviewed=false) is slow; without ordering the re-review
        // would be applied first and then overwritten.
        if (!body.reviewed) await delay(100)
        for (const id of body.ids) server_reviewed.set(id, body.reviewed)
        return HttpResponse.json({ data: { updated: body.ids.length } })
      }),
    )
    const s = useTransactionsStore.getState()
    const undo = s.markReviewed([1, 2], false)
    expect(useTransactionsStore.getState().reviewPending).toBe(1)
    const review = s.markReviewed([1, 2, 3], true)
    expect(useTransactionsStore.getState().reviewPending).toBe(2)
    await Promise.all([undo, review])

    expect(started).toEqual([false, true])
    expect(Object.fromEntries(server_reviewed)).toEqual({ 1: true, 2: true, 3: true })
    expect(useTransactionsStore.getState().reviewPending).toBe(0)
  })

  it('keeps sending later writes after one fails', async () => {
    const bodies: boolean[] = []
    server.use(
      http.post(`${API}/transactions/review`, async ({ request }) => {
        const body = (await request.json()) as { ids: number[]; reviewed: boolean }
        bodies.push(body.reviewed)
        if (!body.reviewed) return HttpResponse.json({ error: 'boom' }, { status: 500 })
        return HttpResponse.json({ data: { updated: body.ids.length } })
      }),
    )
    const s = useTransactionsStore.getState()
    const failed = s.markReviewed([1], false).catch((e: unknown) => e)
    const ok = s.markReviewed([1], true)
    expect(await failed).toBeInstanceOf(Error)
    await ok
    expect(bodies).toEqual([false, true])
    expect(useTransactionsStore.getState().reviewPending).toBe(0)
  })
})
