// TimeAgo renders a relative time string for an ISO/RFC3339 timestamp with
// the exact value available as a tooltip and (for screen readers) on the
// underlying <time> element. Use this anywhere a TIMESTAMPTZ field reaches
// the UI — sync chips, audit logs, "Joined / Updated" labels, etc.
//
// Buckets:
//   < 60s   → "just now"
//   < 60m   → "Xm ago"
//   < 24h   → "Xh ago"
//   < 7d    → weekday name ("Tuesday")
//   < same year → "Mar 14"
//   otherwise   → "Mar 14, 2025"
//
// Future times bucket symmetrically ("in Xm"). Anything older than ~6 months
// falls into the dated branch — bucketed "5mo ago" labels stop being useful
// once you can just show a calendar date.

import type { ReactElement } from 'react'

type Props = {
  when: string | null | undefined
  className?: string
  /** Override Date.now() — only used by tests. */
  now?: () => number
}

export function TimeAgo({ when, className, now }: Props): ReactElement {
  if (!when) {
    return <span className={className}>—</span>
  }
  const t = Date.parse(when)
  if (Number.isNaN(t)) {
    return <span className={className}>—</span>
  }
  const nowMs = (now ?? Date.now)()
  const label = formatRelativeTime(t, nowMs)
  return (
    <time dateTime={when} title={when} className={className}>
      {label}
    </time>
  )
}

function formatRelativeTime(targetMs: number, nowMs: number): string {
  const diffSec = Math.round((targetMs - nowMs) / 1000)
  const absSec = Math.abs(diffSec)

  if (absSec < 60) return diffSec >= 0 ? 'just now' : 'just now'

  const absMin = Math.round(absSec / 60)
  if (absMin < 60) {
    return diffSec < 0 ? `${absMin}m ago` : `in ${absMin}m`
  }

  const absHr = Math.round(absMin / 60)
  if (absHr < 24) {
    return diffSec < 0 ? `${absHr}h ago` : `in ${absHr}h`
  }

  const target = new Date(targetMs)
  const now = new Date(nowMs)
  const absDays = Math.round(absHr / 24)

  if (absDays < 7) {
    // Within a week: weekday name reads more naturally than "3d ago".
    // Past days say "Tuesday"; future days say "Tuesday" too — the year
    // and tooltip disambiguate.
    return new Intl.DateTimeFormat(undefined, { weekday: 'long' }).format(target)
  }

  const sameYear = target.getFullYear() === now.getFullYear()
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    year: sameYear ? undefined : 'numeric',
  }).format(target)
}
