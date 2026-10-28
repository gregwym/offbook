import type { Account } from '../types/account'

// accountTransactionsHref opens Transactions filtered to one account (#657).
// The account lives in the URL so a reload or shared link keeps the filter.
export function accountTransactionsHref(id: number): string {
  return `/transactions?account_id=${id}`
}

// isBrokerage mirrors backend service.brokerageAccountTypes — only these
// account shapes accept trades.
export function isBrokerage(a: Account): boolean {
  return a.account_type === 'investment' || a.account_type === 'crypto'
}

// canUpdateBalance: manual cash-style accounts only. Plaid accounts get their
// balance from sync (the backend refuses them), and a brokerage balance is
// positions × prices, not one number the user can type.
export function canUpdateBalance(a: Account): boolean {
  return !a.plaid_item_id && !a.plaid_account_id && !isBrokerage(a)
}
