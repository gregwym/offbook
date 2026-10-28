import { describe, expect, it } from 'vitest'
import { account1 } from '../test/fixtures'
import type { Account } from '../types/account'
import { groupAccounts, institutionLabel, summarizeAccounts } from './accountGroups'

const acct = (over: Partial<Account>): Account => ({ ...account1, ...over })

describe('groupAccounts', () => {
  it('orders groups by kind, omits empty ones and sums exactly', () => {
    const groups = groupAccounts([
      acct({ id: 1, account_type: 'investment', balance: '1200.000000000000000000' }),
      acct({ id: 2, account_type: 'credit_card', balance: '-12341.70' }),
      acct({ id: 3, account_type: 'savings', balance: '2750.10' }),
      acct({ id: 4, account_type: 'checking', balance: '0.20' }),
      acct({ id: 5, account_type: 'crypto', balance: '0.000000000000000001' }),
    ])
    expect(groups.map((g) => g.key)).toEqual(['cash', 'credit', 'investments'])
    expect(groups[0].accounts.map((a) => a.id)).toEqual([3, 4])
    expect(groups[0].subtotals).toEqual([{ currency: 'USD', amount: '2750.3', complete: true }])
    expect(groups[1].subtotals[0].amount).toBe('-12341.7')
    expect(groups[2].subtotals[0].amount).toBe('1200.000000000000000001')
  })

  it('keeps one subtotal per currency and carries partial', () => {
    const [cash] = groupAccounts([
      acct({ id: 1, balance: '10', currency: 'USD' }),
      acct({ id: 2, balance: '5', currency: 'EUR', balance_complete: false }),
      acct({ id: 3, balance: '2.5', currency: 'USD' }),
    ])
    expect(cash.subtotals).toEqual([
      { currency: 'USD', amount: '12.5', complete: true },
      { currency: 'EUR', amount: '5', complete: false },
    ])
  })

  it('puts loans and other types in their own groups', () => {
    const groups = groupAccounts([acct({ id: 1, account_type: 'other' }), acct({ id: 2, account_type: 'loan' })])
    expect(groups.map((g) => g.label)).toEqual(['Loans', 'Other'])
  })
})

describe('summarizeAccounts', () => {
  it('splits assets from liabilities by account type and nets them', () => {
    const s = summarizeAccounts([
      acct({ id: 1, account_type: 'checking', balance: '20372.30' }),
      acct({ id: 2, account_type: 'savings', balance: '2750.00' }),
      acct({ id: 3, account_type: 'credit_card', balance: '-12341.70' }),
      acct({ id: 4, account_type: 'investment', balance: '1200.00' }),
    ])
    expect(s).toEqual({ currency: 'USD', assets: '24322.3', liabilities: '-12341.7', netWorth: '11980.6', complete: true })
  })

  it('is partial when any balance is', () => {
    expect(summarizeAccounts([acct({ balance_complete: false })])?.complete).toBe(false)
  })

  it('declines a mixed-currency book rather than adding across currencies', () => {
    expect(summarizeAccounts([acct({ id: 1 }), acct({ id: 2, currency: 'EUR' })])).toBeNull()
    expect(summarizeAccounts([])).toBeNull()
  })
})

describe('institutionLabel', () => {
  const names = new Map([['item-1', 'Chase']])
  it('never shows a raw slug or Plaid institution id', () => {
    expect(institutionLabel(acct({ institution_slug: 'manual' }), names)).toBe('Manual')
    expect(institutionLabel(acct({ institution_slug: '' }), names)).toBe('Manual')
    expect(institutionLabel(acct({ institution_slug: 'ins_3', plaid_item_id: 'item-1' }), names)).toBe('Chase')
    expect(institutionLabel(acct({ institution_slug: 'ins_3', plaid_item_id: 'item-2' }), names)).toBe('Linked bank')
    expect(institutionLabel(acct({ institution_slug: 'Ally Bank' }), names)).toBe('Ally Bank')
  })
})
