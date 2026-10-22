import type { Transaction } from '../types/transaction'

// The name a transaction row leads with (#579): the merchant the user fixed
// (#569) or a rule renamed (#534), else the cleaned bank string, else the raw
// one. The raw bank string stays reachable as a secondary line.
export function txDisplayName(t: Pick<Transaction, 'merchant_name' | 'description_clean' | 'description'>): string {
  return (
    t.merchant_name?.trim() ||
    t.description_clean?.trim() ||
    t.description?.trim() ||
    ''
  )
}

// The raw bank description, only when it says something the display name
// doesn't — so a row never repeats the same text twice.
export function txSecondaryDescription(t: Pick<Transaction, 'merchant_name' | 'description_clean' | 'description'>): string | null {
  const raw = t.description?.trim() ?? ''
  if (raw === '') return null
  return raw.toLowerCase() === txDisplayName(t).toLowerCase() ? null : raw
}
