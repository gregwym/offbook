import { describe, expect, it } from 'vitest'
import { reportedBy, sourceLabel } from './balanceSource'

describe('balance source wording (#665)', () => {
  it.each([
    ['manual', 'You', 'by you'],
    ['plaid', 'Your bank', 'by your bank'],
    ['csv', 'Your statement', 'from your statement'],
    ['pdf', 'Your statement', 'from your statement'],
    ['photo', 'Your statement', 'from your statement'],
    ['system', 'Offbook', 'by Offbook'],
    ['mystery', 'Reported', ''],
    [null, 'Reported', ''],
  ])('%s → %s / %s', (source, label, by) => {
    expect(sourceLabel(source)).toBe(label)
    expect(reportedBy(source)).toBe(by)
  })
})
