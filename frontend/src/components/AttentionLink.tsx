import { Link } from 'react-router-dom'
import { AlertTriangle } from 'lucide-react'
import { AmountDisplay } from './AmountDisplay'

type Props = {
  accountID: number
  currency: string
  // attention_count / attention_amount from the account response (#697).
  count?: number
  amount?: string | null
}

// AttentionLink is the Accounts-list "needs attention" affordance (#697).
// The reason is visible text (not a hover-only title, which touch devices
// never show), the destination is part of the link's own name, and the hit
// area is ≥40px on phones; desktop keeps it to one compact line.
export function AttentionLink({ accountID, currency, count, amount }: Props) {
  return (
    <Link
      to={`/accounts/${accountID}/reconciliation`}
      className="mt-1 flex min-h-10 w-fit items-center gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1 text-xs font-normal text-amber-800 hover:bg-amber-100 md:min-h-0 md:py-0.5"
    >
      <AlertTriangle size={14} aria-hidden="true" className="shrink-0" />
      <span>
        <span className="font-semibold">needs attention</span>
        <span aria-hidden="true"> · </span>
        <AttentionReason currency={currency} count={count} amount={amount} />
        <span className="whitespace-nowrap underline underline-offset-2"> — review in Reconciliation</span>
      </span>
    </Link>
  )
}

function AttentionReason({ currency, count, amount }: Omit<Props, 'accountID'>) {
  if (count === 1 && amount) {
    return (
      <>
        Unexplained adjustment of <AmountDisplay amount={amount} currency={currency} />
      </>
    )
  }
  if (count && count > 1) return <>{count} unexplained adjustments</>
  return <>An adjustment doesn’t match your transactions</>
}
