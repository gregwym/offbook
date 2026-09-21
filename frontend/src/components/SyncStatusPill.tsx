import { useState, type ReactNode } from 'react'
import type { Account, SyncStatus } from '../types/account'
import { PartialBadge } from './PartialBadge'
import { TimeAgo } from './TimeAgo'

// Once a Plaid-linked account's last known-good sync is older than this, we
// flag it stale even if last_sync_status still reads "ok"/"ok_with_errors" —
// the daily scheduler (#363) should have refreshed it well before now, so
// silence this long means the schedule isn't reaching this item (crashed
// scheduler, host down, etc.), not that Plaid itself is failing (#365).
const STALE_AFTER_MS = 3 * 24 * 60 * 60 * 1000

function isStale(status: SyncStatus, lastSyncedAt: string | null, nowMs: number): boolean {
  if (status !== 'ok' && status !== 'ok_with_errors') return false
  if (!lastSyncedAt) return false
  const t = Date.parse(lastSyncedAt)
  if (Number.isNaN(t)) return false
  return nowMs - t > STALE_AFTER_MS
}

// SyncStatusPill renders a compact traffic-light indicator for a Plaid-linked
// account. Manual accounts (last_sync_status === null) render nothing — the
// issue spec is explicit that there's no pill for non-Plaid rows.
//
// `error`, `ok_with_errors`, and `reauth_required` are click-to-expand so the
// message stays inline without blowing up the table row by default.
export function SyncStatusPill({ account, now }: { account: Account; now?: () => number }) {
  const [open, setOpen] = useState(false)
  const status = account.last_sync_status
  if (status === null) return null

  const label = pillLabel(status, account.last_synced_at)
  const tone = pillTone(status)
  const expandable =
    (status === 'error' || status === 'ok_with_errors' || status === 'reauth_required') &&
    !!account.last_sync_error
  const stale = isStale(status, account.last_synced_at, (now ?? Date.now)())

  return (
    <div className="inline-flex flex-col items-start gap-1">
      <div className="inline-flex items-center">
        <button
          type="button"
          disabled={!expandable}
          onClick={() => expandable && setOpen((v) => !v)}
          className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ${tone} ${expandable ? 'cursor-pointer hover:brightness-95' : 'cursor-default'}`}
          aria-expanded={expandable ? open : undefined}
        >
          <span aria-hidden="true" className={`h-1.5 w-1.5 rounded-full ${dotTone(status)}`} />
          {label}
        </button>
        {stale && (
          <PartialBadge
            label="stale"
            title="No successful sync in over 3 days — the scheduled background sync may not be running for this item."
          />
        )}
      </div>
      {expandable && open && (
        <div className="max-w-xs whitespace-pre-wrap rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-700">
          {account.last_sync_error}
        </div>
      )}
    </div>
  )
}

function pillLabel(status: NonNullable<Account['last_sync_status']>, lastSyncedAt: string | null): ReactNode {
  switch (status) {
    case 'syncing':
      return 'Syncing…'
    case 'error':
      return 'Sync failed'
    case 'reauth_required':
      return 'Needs reconnect'
    case 'never':
      return 'Not synced'
    case 'ok':
      return lastSyncedAt ? (
        <>Synced <TimeAgo when={lastSyncedAt} /></>
      ) : 'Synced'
    case 'ok_with_errors':
      return lastSyncedAt ? (
        <>Synced <TimeAgo when={lastSyncedAt} /> (errors)</>
      ) : 'Synced (errors)'
  }
}

function pillTone(status: NonNullable<Account['last_sync_status']>): string {
  switch (status) {
    case 'ok':
      return 'bg-emerald-100 text-emerald-800'
    case 'ok_with_errors':
      return 'bg-amber-100 text-amber-800'
    case 'syncing':
      return 'bg-amber-100 text-amber-800'
    case 'error':
      return 'bg-red-100 text-red-800'
    case 'reauth_required':
      return 'bg-amber-100 text-amber-800'
    case 'never':
      return 'bg-gray-100 text-gray-700'
  }
}

function dotTone(status: NonNullable<Account['last_sync_status']>): string {
  switch (status) {
    case 'ok':
      return 'bg-emerald-500'
    case 'ok_with_errors':
      return 'bg-amber-500'
    case 'syncing':
      return 'bg-amber-500'
    case 'error':
      return 'bg-red-500'
    case 'reauth_required':
      return 'bg-amber-500'
    case 'never':
      return 'bg-gray-400'
  }
}
