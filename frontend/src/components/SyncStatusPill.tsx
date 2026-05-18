import { useState } from 'react'
import type { Account } from '../types/account'

// SyncStatusPill renders a compact traffic-light indicator for a Plaid-linked
// account. Manual accounts (last_sync_status === null) render nothing — the
// issue spec is explicit that there's no pill for non-Plaid rows.
//
// `error` is click-to-expand so the message stays inline without blowing up
// the table row by default. Other states have no expansion target.
export function SyncStatusPill({ account }: { account: Account }) {
  const [open, setOpen] = useState(false)
  const status = account.last_sync_status
  if (status === null) return null

  const label = pillLabel(status, account.last_synced_at)
  const tone = pillTone(status)
  const expandable = status === 'error' && account.last_sync_error

  return (
    <div className="inline-flex flex-col items-start gap-1">
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
      {expandable && open && (
        <div className="max-w-xs whitespace-pre-wrap rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs text-red-700">
          {account.last_sync_error}
        </div>
      )}
    </div>
  )
}

function pillLabel(status: NonNullable<Account['last_sync_status']>, lastSyncedAt: string | null): string {
  switch (status) {
    case 'syncing':
      return 'Syncing…'
    case 'error':
      return 'Sync failed'
    case 'never':
      return 'Not synced'
    case 'ok':
      return lastSyncedAt ? `Synced ${formatRelative(lastSyncedAt)}` : 'Synced'
  }
}

function pillTone(status: NonNullable<Account['last_sync_status']>): string {
  switch (status) {
    case 'ok':
      return 'bg-emerald-100 text-emerald-800'
    case 'syncing':
      return 'bg-amber-100 text-amber-800'
    case 'error':
      return 'bg-red-100 text-red-800'
    case 'never':
      return 'bg-gray-100 text-gray-700'
  }
}

function dotTone(status: NonNullable<Account['last_sync_status']>): string {
  switch (status) {
    case 'ok':
      return 'bg-emerald-500'
    case 'syncing':
      return 'bg-amber-500'
    case 'error':
      return 'bg-red-500'
    case 'never':
      return 'bg-gray-400'
  }
}

// formatRelative renders a coarse "Xm/h/d ago" — enough resolution for a
// sync indicator. Anything older than 30 days falls back to a calendar date
// so the pill doesn't claim "1y ago" forever.
function formatRelative(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return 'recently'
  const diffSec = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (diffSec < 60) return 'just now'
  const mins = Math.floor(diffSec / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  return new Date(iso).toLocaleDateString()
}
