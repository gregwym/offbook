// Rows for a goal's contribution history (#682). The API lists recorded
// contributions newest first, each with the goal's total right after it.
// Money a goal held before history was kept has no rows of its own; it shows
// as one "before history" line so the running totals still add up to the
// goal's current amount.
import type { GoalContribution } from '../types/savingsGoal'
import { subDecimal } from './decimal'

export type GoalHistoryRow =
  | { kind: 'entry'; id: number; at: string; amount: string; total: string }
  | { kind: 'before'; total: string }

export function goalHistoryRows(items: GoalContribution[], currentAmount: string): GoalHistoryRow[] {
  const rows: GoalHistoryRow[] = items.map((c) => ({
    kind: 'entry',
    id: c.id,
    at: c.created_at,
    amount: subDecimal(c.amount, '0'),
    total: subDecimal(c.balance_after, '0'),
  }))
  const oldest = items[items.length - 1]
  const before = oldest ? subDecimal(oldest.balance_after, oldest.amount) : subDecimal(currentAmount, '0')
  if (before !== '0') rows.push({ kind: 'before', total: before })
  return rows
}
