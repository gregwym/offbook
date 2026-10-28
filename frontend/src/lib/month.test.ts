import { describe, expect, it } from 'vitest'
import { monthTicks, rangeLabel } from './month'

describe('rangeLabel', () => {
  it('reads an exclusive month window as an inclusive range', () => {
    expect(rangeLabel('2026-10-01', '2026-11-01')).toBe('Oct 1 – Oct 31, 2026')
    expect(rangeLabel('2026-02-01T00:00:00Z', '2026-03-01T00:00:00Z')).toBe('Feb 1 – Feb 28, 2026')
  })

  it('names both years when the window crosses one', () => {
    expect(rangeLabel('2025-12-15', '2026-01-15')).toBe('Dec 15, 2025 – Jan 14, 2026')
  })
})

describe('monthTicks', () => {
  it('uses short month names and names the year first and where it changes', () => {
    expect(monthTicks(['2025-11', '2025-12', '2026-01', '2026-02'])).toEqual([
      { month: 'Nov', year: '2025' },
      { month: 'Dec', year: null },
      { month: 'Jan', year: '2026' },
      { month: 'Feb', year: null },
    ])
  })
})
