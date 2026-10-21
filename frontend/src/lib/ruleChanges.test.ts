import { describe, expect, it } from 'vitest'
import { describeManualPicks, describeRuleChanges, formatApplyResult, type RuleChangeCounts } from './ruleChanges'

const none: RuleChangeCounts = { category: 0, renamed: 0, hidden: 0, reviewed: 0, tagged: 0 }

describe('describeRuleChanges (#552)', () => {
  it.each<[string, Partial<RuleChangeCounts>, string, string]>([
    ['category', { category: 3 }, '3 would change category', '3 changed category'],
    ['rename only', { renamed: 2 }, '2 would be renamed', '2 renamed'],
    ['hide only', { hidden: 1 }, '1 would be hidden', '1 hidden'],
    ['review only', { reviewed: 4 }, '4 would be marked reviewed', '4 marked reviewed'],
    ['tag only', { tagged: 2 }, '2 would be tagged', '2 tagged'],
    [
      'every action, zero parts omitted',
      { category: 3, renamed: 2, hidden: 1, tagged: 2 },
      '3 would change category, 2 renamed, 1 hidden, 2 tagged',
      '3 changed category, 2 renamed, 1 hidden, 2 tagged',
    ],
    ['nothing visible', {}, '', ''],
  ])('%s', (_, counts, would, did) => {
    expect(describeRuleChanges({ ...none, ...counts }, 'would')).toBe(would)
    expect(describeRuleChanges({ ...none, ...counts }, 'did')).toBe(did)
  })

  it('words manual picks by number and tense', () => {
    expect(describeManualPicks(0, 'would')).toBe('')
    expect(describeManualPicks(1, 'would')).toBe('1 manual pick stays as it is')
    expect(describeManualPicks(2, 'would')).toBe('2 manual picks stay as they are')
    expect(describeManualPicks(1, 'did')).toBe('1 manual pick stayed as it was')
  })
})

describe('formatApplyResult (#552)', () => {
  const base = { scanned: 40, updated: 0, skipped_manual: 0 }
  it.each<[string, Parameters<typeof formatApplyResult>[0], string]>([
    [
      'rename-only preview never says re-categorized',
      { ...base, updated: 2, renamed: 2, dry_run: true },
      'Preview (2 of 40 transactions): 2 would be renamed. Nothing has been changed.',
    ],
    [
      'mixed apply with a manual pick',
      { ...base, updated: 3, skipped_manual: 1, recategorized: 3, renamed: 2, hidden: 1, tagged: 2 },
      'Updated 3 of 40 transactions: 3 changed category, 2 renamed, 1 hidden, 2 tagged; 1 manual pick stayed as it was.',
    ],
    [
      'nothing to change',
      { ...base, skipped_manual: 2, dry_run: true },
      'Preview: none of 40 transactions would change; 2 manual picks stay as they are. Nothing has been changed.',
    ],
    ['apply with nothing to do', { ...base }, 'Nothing to update in 40 transactions.'],
    [
      'provenance-only change',
      { ...base, updated: 1 },
      'Updated 1 of 40 transactions: they now record a rule as their source.',
    ],
  ])('%s', (_, result, want) => {
    expect(formatApplyResult(result)).toBe(want)
  })
})
