// spendingHref builds the Transactions drill-down link behind a spending
// figure (#571): ?spending=1 lists exactly the rows that figure sums, and
// the page shows their total. Periods arrive as the API's [start, end)
// window (RFC3339 or YYYY-MM-DD); the list's `to` is inclusive, so the
// link ends the day before `end`.
export function spendingHref(opts: {
  start: string
  end: string
  // null = uncategorized; undefined = every category.
  categoryID?: number | null
  // With categoryID: its subcategories a group budget also counts (#596).
  subcategoryIDs?: number[]
  merchant?: string
}): string {
  const params = new URLSearchParams({ spending: '1' })
  if (opts.categoryID === null) params.set('category_id', 'null')
  else if (opts.categoryID !== undefined) params.set('category_id', String(opts.categoryID))
  if (opts.categoryID != null && opts.subcategoryIDs?.length) params.set('subcategory_ids', opts.subcategoryIDs.join(','))
  if (opts.merchant) params.set('merchant', opts.merchant)
  params.set('from', opts.start.slice(0, 10))
  params.set('to', dayBefore(opts.end.slice(0, 10)))
  return `/transactions?${params.toString()}`
}

// dayBefore is calendar arithmetic on a YYYY-MM-DD string, in UTC so no
// local timezone can shift it.
export function dayBefore(date: string): string {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() - 1)
  return d.toISOString().slice(0, 10)
}
