import { describe, expect, it } from 'vitest'
import { txDisplayName, txSecondaryDescription } from './txName'

describe('txDisplayName', () => {
  it('prefers the merchant, then the cleaned description, then the raw one', () => {
    expect(txDisplayName({ merchant_name: 'Blue Bottle', description_clean: 'Sq Blue Btl', description: 'SQ *BLUE BTL 0415' })).toBe('Blue Bottle')
    expect(txDisplayName({ merchant_name: '  ', description_clean: 'Sq Blue Btl', description: 'SQ *BLUE BTL 0415' })).toBe('Sq Blue Btl')
    expect(txDisplayName({ merchant_name: null, description_clean: null, description: 'SQ *BLUE BTL 0415' })).toBe('SQ *BLUE BTL 0415')
    expect(txDisplayName({})).toBe('')
  })
})

describe('txSecondaryDescription', () => {
  it('returns the raw bank string only when it differs from the name', () => {
    expect(txSecondaryDescription({ merchant_name: 'Blue Bottle', description: 'SQ *BLUE BTL 0415' })).toBe('SQ *BLUE BTL 0415')
    expect(txSecondaryDescription({ merchant_name: 'NETFLIX', description: 'Netflix' })).toBeNull()
    expect(txSecondaryDescription({ description: 'SQ *BLUE BTL 0415' })).toBeNull()
    expect(txSecondaryDescription({ merchant_name: 'Blue Bottle' })).toBeNull()
  })
})
