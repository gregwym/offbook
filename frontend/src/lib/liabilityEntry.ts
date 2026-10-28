// Manual balance entry for liabilities (#667). A credit card or loan
// statement shows what the user owes as a positive figure, and that is what
// the forms ask for. The API stores liabilities negative (#369), so the value
// is negated on the way in and out. A negative entry means the account is in
// credit. Assets pass through unchanged. Exact decimal strings throughout.
import { isLiabilityType } from '../types/account'
import { subDecimal } from './decimal'

// ledgerFromEntry turns what the user typed into the signed balance the API
// expects.
export function ledgerFromEntry(accountType: string, entered: string): string {
  return isLiabilityType(accountType) ? subDecimal('0', entered) : entered
}

// entryFromLedger turns a stored balance into the value a form pre-fills.
export function entryFromLedger(accountType: string, balance: string): string {
  return isLiabilityType(accountType) ? subDecimal('0', balance) : subDecimal(balance, '0')
}

// liabilityReading says how a liability entry will be read, for the live
// line under the field. null when the entry isn't a number yet.
export function liabilityReading(entered: string): { kind: 'owed' | 'credit' | 'zero'; amount: string } | null {
  const v = entered.replace(/[,\s$]/g, '')
  if (!/^-?\d+(\.\d+)?$/.test(v)) return null
  const n = subDecimal(v, '0')
  if (n === '0' || n === '-0') return { kind: 'zero', amount: '0' }
  if (n.startsWith('-')) return { kind: 'credit', amount: n.slice(1) }
  return { kind: 'owed', amount: n }
}
