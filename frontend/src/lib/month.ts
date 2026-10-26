// Month helpers on "YYYY-MM" strings. Months are UTC calendar months — the
// server's budget and spending windows are UTC.
export function currentMonth(): string {
  return new Date().toISOString().slice(0, 7)
}

export function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1 + delta, 1)).toISOString().slice(0, 7)
}

export function monthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, 1)).toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
}

// monthWindow is the month's [from, to) window as YYYY-MM-DD dates.
export function monthWindow(month: string): { from: string; to: string } {
  return { from: `${month}-01`, to: `${shiftMonth(month, 1)}-01` }
}

// rangeLabel reads a [from, to) window (YYYY-MM-DD or RFC3339) as an
// inclusive human range: "Oct 1 – Oct 31, 2026", or with both years when
// the window crosses one ("Dec 1, 2025 – Jan 31, 2026").
export function rangeLabel(from: string, to: string): string {
  const start = new Date(`${from.slice(0, 10)}T00:00:00Z`)
  const end = new Date(`${to.slice(0, 10)}T00:00:00Z`)
  end.setUTCDate(end.getUTCDate() - 1)
  const day = (d: Date, year: boolean) =>
    d.toLocaleString('en-US', { month: 'short', day: 'numeric', ...(year ? { year: 'numeric' } : {}), timeZone: 'UTC' })
  const sameYear = start.getUTCFullYear() === end.getUTCFullYear()
  return `${day(start, !sameYear)} – ${day(end, true)}`
}
