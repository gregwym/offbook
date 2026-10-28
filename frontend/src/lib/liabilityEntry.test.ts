import { describe, expect, it } from 'vitest'
import { entryFromLedger, ledgerFromEntry, liabilityReading } from './liabilityEntry'

describe('liability balance entry (#667)', () => {
  it('maps between what the user types and the stored balance', () => {
    const cases: { type: string; entered: string; ledger: string }[] = [
      { type: 'credit_card', entered: '450.00', ledger: '-450' },
      { type: 'loan', entered: '12341.70', ledger: '-12341.7' },
      { type: 'credit_card', entered: '-20.5', ledger: '20.5' },
      { type: 'credit_card', entered: '0', ledger: '0' },
      { type: 'checking', entered: '1300.00', ledger: '1300.00' },
      { type: 'checking', entered: '-35.10', ledger: '-35.10' },
    ]
    for (const c of cases) {
      expect(ledgerFromEntry(c.type, c.entered), `${c.type} ${c.entered}`).toBe(c.ledger)
    }
  })

  it('pre-fills a liability with the amount owed, an asset with its trimmed balance', () => {
    expect(entryFromLedger('credit_card', '-450.000000000000000000')).toBe('450')
    expect(entryFromLedger('credit_card', '20.000000000000000000')).toBe('-20')
    expect(entryFromLedger('credit_card', '0.000000000000000000')).toBe('0')
    expect(entryFromLedger('checking', '1234.560000000000000000')).toBe('1234.56')
    // Exact at crypto scale: no float round trip.
    expect(entryFromLedger('loan', '-0.000000000000000001')).toBe('0.000000000000000001')
  })

  it('says how an entry will be read', () => {
    expect(liabilityReading('1,250.50')).toEqual({ kind: 'owed', amount: '1250.5' })
    expect(liabilityReading('-20')).toEqual({ kind: 'credit', amount: '20' })
    expect(liabilityReading('0.00')).toEqual({ kind: 'zero', amount: '0' })
    expect(liabilityReading('abc')).toBeNull()
    expect(liabilityReading('')).toBeNull()
  })
})
