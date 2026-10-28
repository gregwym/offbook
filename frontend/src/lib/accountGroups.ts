// Accounts page grouping (#655): accounts by kind, each group with an exact
// decimal subtotal, plus the assets / liabilities / net worth summary. Money
// stays as decimal strings throughout (addDecimal), never Number.
import { addDecimal, subDecimal } from './decimal'
import { isLiabilityType, type Account, type AccountType } from '../types/account'

export type AccountGroupKey = 'cash' | 'credit' | 'loans' | 'investments' | 'other'

// CurrencyTotal is one currency's sum. complete=false when any balance in it
// is a partial valuation, so the total must carry the partial badge too.
export type CurrencyTotal = { currency: string; amount: string; complete: boolean }

export type AccountGroup = {
  key: AccountGroupKey
  label: string
  accounts: Account[]
  // One entry per currency, in first-seen order; a single-currency group
  // has exactly one.
  subtotals: CurrencyTotal[]
}

const GROUPS: { key: AccountGroupKey; label: string; types: AccountType[] }[] = [
  { key: 'cash', label: 'Cash', types: ['checking', 'savings', 'cash'] },
  { key: 'credit', label: 'Credit cards', types: ['credit_card'] },
  { key: 'loans', label: 'Loans', types: ['loan'] },
  { key: 'investments', label: 'Investments', types: ['investment', 'crypto'] },
  { key: 'other', label: 'Other', types: ['other'] },
]

// groupAccounts returns the non-empty groups in display order. Account order
// within a group is the order given. An unknown type lands in Other.
export function groupAccounts(accounts: Account[]): AccountGroup[] {
  const known = new Set(GROUPS.flatMap((g) => g.types))
  return GROUPS.map((g) => {
    const members = accounts.filter((a) =>
      g.key === 'other' ? !known.has(a.account_type) || a.account_type === 'other' : g.types.includes(a.account_type),
    )
    return { key: g.key, label: g.label, accounts: members, subtotals: totalsByCurrency(members) }
  }).filter((g) => g.accounts.length > 0)
}

export function totalsByCurrency(accounts: Account[]): CurrencyTotal[] {
  const out: CurrencyTotal[] = []
  for (const a of accounts) {
    let t = out.find((x) => x.currency === a.currency)
    if (!t) {
      t = { currency: a.currency, amount: '0', complete: true }
      out.push(t)
    }
    t.amount = addDecimal(t.amount, a.balance)
    t.complete = t.complete && a.balance_complete
  }
  return out
}

// AccountsSummary is the headline above the groups. Only defined when every
// account shares one currency: converting across currencies is Insights'
// job (it has the FX rates), so a mixed book links there instead.
export type AccountsSummary = {
  currency: string
  assets: string
  liabilities: string // the ledger sum of liability balances (negative when owed)
  // owed is the headline figure (#659): what the user owes as a positive
  // magnitude, so "Liabilities" never reads as a double negative. When the
  // liability accounts net to a credit (liabilities > 0) owed is that credit
  // amount and liabilityCredit is true, so the tile can say "credit" instead
  // of silently flipping the sign.
  owed: string
  liabilityCredit: boolean
  netWorth: string
  complete: boolean
}

export function summarizeAccounts(accounts: Account[]): AccountsSummary | null {
  if (accounts.length === 0) return null
  const currency = accounts[0].currency
  if (accounts.some((a) => a.currency !== currency)) return null
  let assets = '0'
  let liabilities = '0'
  for (const a of accounts) {
    if (isLiabilityType(a.account_type)) liabilities = addDecimal(liabilities, a.balance)
    else assets = addDecimal(assets, a.balance)
  }
  const credit = !liabilities.startsWith('-') && liabilities !== '0'
  return {
    currency,
    assets,
    liabilities,
    owed: credit ? liabilities : subDecimal('0', liabilities),
    liabilityCredit: credit,
    netWorth: addDecimal(assets, liabilities),
    complete: accounts.every((a) => a.balance_complete),
  }
}

// institutionLabel names where an account lives in the user's words: the
// linked institution's name for a Plaid account, "Manual" for one the user
// added. A raw Plaid institution id ("ins_3") is never shown.
export function institutionLabel(a: Account, plaidNames: Map<string, string>): string {
  if (a.plaid_item_id) return plaidNames.get(a.plaid_item_id) ?? 'Linked bank'
  const slug = a.institution_slug.trim()
  if (slug === '' || slug.toLowerCase() === 'manual') return 'Manual'
  if (/^ins_\d+$/.test(slug)) return 'Linked bank'
  return slug
}
