// LiabilityReading (#667): the live line under a credit card / loan balance
// field saying how the entry will be read, so a positive "amount owed" and a
// negative "in credit" entry are never re-signed silently.
import { AmountDisplay } from './AmountDisplay'
import { liabilityReading } from '../lib/liabilityEntry'

export function LiabilityReading({ id, entered, currency }: { id?: string; entered: string; currency: string }) {
  const r = liabilityReading(entered)
  return (
    <span id={id} aria-live="polite" className="mt-1 block text-[11px] text-gray-500">
      {r?.kind === 'owed' ? (
        <>
          You owe <AmountDisplay amount={r.amount} currency={currency} className="font-medium text-gray-700" />.
        </>
      ) : r?.kind === 'credit' ? (
        <>
          In credit by <AmountDisplay amount={r.amount} currency={currency} className="font-medium text-gray-700" /> (the
          lender owes you).
        </>
      ) : r?.kind === 'zero' ? (
        'Nothing owed.'
      ) : (
        'Enter what your statement shows you owe, e.g. 450.00.'
      )}
    </span>
  )
}
