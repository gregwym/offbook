import { describe, expect, it } from 'vitest'
import { reportedDay, reportedLine } from './reportedBalance'

const today = '2026-10-31'
const base = { account_type: 'checking' as const, balance: '900' }

describe('reportedLine (#673)', () => {
  it.each([
    ['no observation', { last_observed_at: null, last_observed_balance: null }, { kind: 'none' }],
    ['same-day match', { last_observed_at: '2026-10-31T14:02:11Z', last_observed_balance: '900.00' }, { kind: 'when' }],
    ['holding observation (no cash figure)', { last_observed_at: '2026-08-30T00:00:00Z', last_observed_balance: null }, { kind: 'when' }],
    ['past-day match', { last_observed_at: '2026-08-30T00:00:00Z', last_observed_balance: '900' }, { kind: 'amount', amount: '900', day: '2026-08-30' }],
    ['past-day mismatch', { last_observed_at: '2026-08-30T00:00:00Z', last_observed_balance: '1100.000000000000000000' }, { kind: 'amount', amount: '1100', day: '2026-08-30' }],
    ['same-day, balance moved since', { last_observed_at: '2026-10-31T14:02:11Z', last_observed_balance: '950' }, { kind: 'amount', amount: '950', day: null }],
  ])('%s', (_, obs, want) => {
    expect(reportedLine({ ...base, ...obs }, today)).toMatchObject(want)
  })

  it('reads a liability report as the amount owed', () => {
    expect(reportedLine({ account_type: 'credit_card' as const, balance: '-450', last_observed_at: '2026-08-30T00:00:00Z', last_observed_balance: '-512.30' }, today))
      .toMatchObject({ kind: 'amount', amount: '512.3', reading: 'owed' })
  })

  it('treats a midnight-UTC report of today as now', () => {
    expect(reportedDay('2026-10-31T00:00:00Z', today)).toBeNull()
  })
})
