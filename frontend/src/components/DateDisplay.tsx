// DateDisplay renders a pure DATE string ("YYYY-MM-DD") in the user's locale,
// without any timezone interpretation. Use this for any column backed by a
// SQL DATE — `transaction_date`, `posted_date`, `snapshot_date`,
// `target_date`, etc. — never new Date(iso).toLocaleDateString(), which
// re-interprets the date in the local timezone and can shift a "2026-05-19"
// row to May 18 for negative-UTC users.

import type { ReactElement } from 'react'

type Props = {
  value: string | null | undefined
  className?: string
  /** Hide the year when it matches the current calendar year. Default true. */
  hideYearWhenCurrent?: boolean
  /** Override "now" — only used by tests. */
  now?: () => Date
}

export function DateDisplay({
  value,
  className,
  hideYearWhenCurrent = true,
  now,
}: Props): ReactElement {
  if (!value) return <span className={className}>—</span>

  const parts = value.split('-')
  if (parts.length !== 3) return <span className={className}>{value}</span>

  const year = Number(parts[0])
  const month = Number(parts[1])
  const day = Number(parts[2])
  if (
    !Number.isFinite(year) ||
    !Number.isFinite(month) ||
    !Number.isFinite(day) ||
    month < 1 ||
    month > 12 ||
    day < 1 ||
    day > 31
  ) {
    return <span className={className}>{value}</span>
  }

  // Construct the Date at noon UTC to avoid any DST/offset edge causing the
  // local-time conversion to round down to the previous day. The fields we
  // pull out below use UTC accessors so this is timezone-stable.
  const dt = new Date(Date.UTC(year, month - 1, day, 12, 0, 0))
  const currentYear = (now ?? (() => new Date()))().getFullYear()
  const showYear = !hideYearWhenCurrent || year !== currentYear

  const label = new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    year: showYear ? 'numeric' : undefined,
    timeZone: 'UTC',
  }).format(dt)

  return (
    <time dateTime={value} title={value} className={className}>
      {label}
    </time>
  )
}
