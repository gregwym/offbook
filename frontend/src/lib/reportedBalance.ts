// How the Accounts row describes the latest balance report (#673). A report
// of today's balance that still matches reads "Reported just now by you". A
// report for an earlier day, or one the current balance has moved away from,
// says what was reported and for which day: "You reported 1,100.00 for
// Aug 30". The current balance sits right above, so pairing a bare date with
// it would claim the user reported a figure they never typed.
import type { Account } from '../types/account'
import { isLiabilityType } from '../types/account'
import { liabilityBalance } from './liabilityEntry'
import { subDecimal } from './decimal'

export type ReportedLine =
  | { kind: 'none' }
  | { kind: 'when'; at: string }
  | { kind: 'amount'; at: string; amount: string; reading: '' | 'owed' | 'credit'; day: string | null }

type Fields = Pick<Account, 'account_type' | 'balance' | 'last_observed_at' | 'last_observed_balance'>

export function reportedLine(a: Fields, today: string = localToday()): ReportedLine {
  const at = a.last_observed_at
  if (!at) return { kind: 'none' }
  const day = reportedDay(at, today)
  const reported = a.last_observed_balance
  if (reported == null) return { kind: 'when', at }
  const moved = subDecimal(reported, a.balance).replace(/^-/, '') !== '0'
  if (!day && !moved) return { kind: 'when', at }
  if (isLiabilityType(a.account_type)) {
    const r = liabilityBalance(reported)
    return { kind: 'amount', at, amount: r.amount, reading: r.kind === 'zero' ? '' : r.kind, day }
  }
  return { kind: 'amount', at, amount: subDecimal(reported, '0'), reading: '', day }
}

// reportedDay is the YYYY-MM-DD a report is for, or null when it was taken
// "now". A past-dated report is pinned to midnight UTC of its day by the API
// (#669); a report of today's balance carries the time it was made.
export function reportedDay(at: string, today: string = localToday()): string | null {
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return null
  const pinned = d.getUTCHours() === 0 && d.getUTCMinutes() === 0 && d.getUTCSeconds() === 0 && d.getUTCMilliseconds() === 0
  if (!pinned) return null
  const iso = d.toISOString().slice(0, 10)
  return iso < today ? iso : null
}

// formatReportedDay renders a YYYY-MM-DD as "Aug 30", adding the year when it
// isn't the current one.
export function formatReportedDay(iso: string, today: string = localToday()): string {
  const [y, m, d] = iso.split('-').map(Number)
  const sameYear = iso.slice(0, 4) === today.slice(0, 4)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, sameYear
    ? { month: 'short', day: 'numeric' }
    : { month: 'short', day: 'numeric', year: 'numeric' })
}

function localToday(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
