// PartialBadge marks a monetary figure whose valuation is incomplete —
// some position had no price (or only a stale one), so the number shown
// is a partial sum, not the whole story (#282/#339). Always render it
// next to the affected amount; never silently show a partial total.
export function PartialBadge({ title }: { title?: string }) {
  return (
    <span
      title={
        title ??
        'Partial value — some assets have no recent price, so this figure may understate the true amount.'
      }
      className="ml-1.5 inline-flex cursor-help items-center rounded border border-amber-200 bg-amber-50 px-1 py-px align-middle text-[10px] font-medium leading-4 text-amber-700"
    >
      partial
    </span>
  )
}
