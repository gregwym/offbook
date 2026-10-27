import { AlertTriangle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { listItems } from '../api/plaid'
import { RECONNECT_PATH } from '../lib/plaidReconnect'
import type { PlaidItem } from '../types/plaid'
import { TimeAgo } from './TimeAgo'

// ReconnectBanner (#641): a bank in reauth_required stops syncing until the
// user signs in again through Plaid Link, so every personal page says so —
// not just Settings — instead of quietly showing numbers that stopped
// updating. Refetches on navigation so a finished reconnect clears it.
// Settings itself hosts the Reconnect button, so the banner stays out of it.
// A failed fetch renders nothing: this is a secondary signal and must never
// blank the page it sits on.
export function ReconnectBanner() {
  const { pathname } = useLocation()
  const [items, setItems] = useState<PlaidItem[]>([])
  const onSettings = pathname === '/settings'

  useEffect(() => {
    if (onSettings) return
    let cancelled = false
    listItems()
      .then((r) => {
        if (!cancelled) setItems(r.items.filter((it) => it.last_sync_status === 'reauth_required'))
      })
      .catch(() => {
        if (!cancelled) setItems([])
      })
    return () => {
      cancelled = true
    }
  }, [pathname, onSettings])

  if (onSettings || items.length === 0) return null

  const first = items[0]
  const name = first.institution_name ?? 'A linked bank'
  const title =
    items.length === 1 ? `${name} needs you to sign in again` : `${items.length} banks need you to sign in again`

  return (
    <div
      role="status"
      className="mb-4 flex flex-col gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 sm:flex-row sm:items-center md:mb-6"
    >
      <AlertTriangle size={18} className="hidden shrink-0 text-amber-600 sm:block" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <div className="font-medium">{title}</div>
        <div className="mt-0.5 text-amber-800">
          {items.length === 1 && first.last_synced_at ? (
            <>
              Last synced <TimeAgo when={first.last_synced_at} />.{' '}
            </>
          ) : null}
          Balances and transactions from {items.length === 1 ? 'it' : 'them'} stop updating until you reconnect.
        </div>
      </div>
      <Link
        to={RECONNECT_PATH}
        className="inline-flex min-h-[44px] shrink-0 items-center justify-center rounded-md bg-amber-600 px-4 text-sm font-medium text-white hover:bg-amber-700 md:min-h-0 md:py-1.5"
      >
        Reconnect
      </Link>
    </div>
  )
}
