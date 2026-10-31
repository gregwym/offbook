// AccountBalance (#674): an account's balance the way the user reads it. An
// asset shows its signed balance. A credit card or loan shows what is owed
// as a positive amount, the same figure the Accounts summary tile and the
// Update balance form use; one in credit says "credit" instead of relying on
// a bare sign. Display only: the API keeps liabilities negative (#369).
import { AmountDisplay } from './AmountDisplay'
import { isLiabilityType } from '../types/account'
import { liabilityBalance } from '../lib/liabilityEntry'

export function AccountBalance({ accountType, liability, balance, currency }: {
  // Either the account's type, or liability=true for a liabilities-only
  // subtotal (Credit cards / Loans groups).
  accountType?: string
  liability?: boolean
  balance: string
  currency: string
}) {
  if (!(liability ?? isLiabilityType(accountType ?? ''))) {
    return <AmountDisplay amount={balance} currency={currency} />
  }
  const r = liabilityBalance(balance)
  if (r.kind === 'credit') {
    return (
      <span title="In credit: the lender owes you">
        <AmountDisplay amount={r.amount} currency={currency} />{' '}
        <span className="text-xs font-normal text-gray-500">credit</span>
      </span>
    )
  }
  return <AmountDisplay amount={r.amount} currency={currency} />
}
