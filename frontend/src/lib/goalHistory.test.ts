import { describe, expect, it } from 'vitest'
import { goalHistoryRows } from './goalHistory'

const c = (id: number, amount: string, balance_after: string) => ({
  id, goal_id: 1, amount, balance_after, created_at: '2026-10-30T12:00:00Z',
})

describe('goalHistoryRows (#682)', () => {
  it('lists entries newest first with exact totals and no before-line when history starts at zero', () => {
    const rows = goalHistoryRows(
      [c(2, '-100.500000000000000000', '149.750000000000000001'), c(1, '250.250000000000000001', '250.250000000000000001')],
      '149.750000000000000001',
    )
    expect(rows).toEqual([
      { kind: 'entry', id: 2, at: '2026-10-30T12:00:00Z', amount: '-100.5', total: '149.750000000000000001' },
      { kind: 'entry', id: 1, at: '2026-10-30T12:00:00Z', amount: '250.250000000000000001', total: '250.250000000000000001' },
    ])
  })

  it('adds one before-history line for money saved before history was kept', () => {
    const rows = goalHistoryRows([c(1, '19.60', '320.00')], '320.00')
    expect(rows[rows.length - 1]).toEqual({ kind: 'before', total: '300.4' })
  })

  it('shows only the before-history line when a funded goal has no recorded entries', () => {
    expect(goalHistoryRows([], '200.000000000000000000')).toEqual([{ kind: 'before', total: '200' }])
  })

  it('is empty for a new goal', () => {
    expect(goalHistoryRows([], '0')).toEqual([])
  })
})
