import { describe, expect, it } from 'vitest'
import { spendingHref } from './spendingLink'

describe('spendingHref', () => {
  it('turns an exclusive month window into inclusive dates', () => {
    expect(spendingHref({ start: '2026-10-01T00:00:00-04:00', end: '2026-11-01T00:00:00-04:00', categoryID: 7 })).toBe(
      '/transactions?spending=1&category_id=7&from=2026-10-01&to=2026-10-31',
    )
  })

  it('handles uncategorized, merchants and year boundaries', () => {
    expect(spendingHref({ start: '2026-12-01', end: '2027-01-01', categoryID: null })).toBe(
      '/transactions?spending=1&category_id=null&from=2026-12-01&to=2026-12-31',
    )
    expect(spendingHref({ start: '2026-03-01', end: '2026-04-01', merchant: 'Trader Joe & Co' })).toBe(
      '/transactions?spending=1&merchant=Trader+Joe+%26+Co&from=2026-03-01&to=2026-03-31',
    )
  })
})
