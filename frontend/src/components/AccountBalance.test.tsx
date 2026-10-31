import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AccountBalance } from './AccountBalance'

// #674: a liability reads as the amount owed; a credit says so; assets keep
// their sign.
describe('AccountBalance', () => {
  const text = (el: React.ReactElement) => render(el).container.textContent

  it('shows a liability as the amount owed, a credit as credit, zero as zero', () => {
    expect(text(<AccountBalance accountType="credit_card" balance="-12341.70" currency="USD" />)).toBe('12,341.70 USD')
    expect(text(<AccountBalance accountType="loan" balance="20.5" currency="USD" />)).toBe('20.50 USD credit')
    expect(text(<AccountBalance accountType="credit_card" balance="0.000000000000000000" currency="USD" />)).toBe('0.00 USD')
    expect(text(<AccountBalance liability balance="-450" currency="USD" />)).toBe('450.00 USD')
  })

  it('leaves assets signed', () => {
    expect(text(<AccountBalance accountType="checking" balance="-35.10" currency="USD" />)).toBe('-35.10 USD')
    expect(text(<AccountBalance accountType="crypto" balance="0.000000000000000001" currency="ETH" />)).toBe('0.000000 ETH')
  })
})
