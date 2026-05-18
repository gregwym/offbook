import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Landmark, Plug, Trash2 } from 'lucide-react'
import { disconnectItem, listItems } from '../api/plaid'
import type { PlaidItem } from '../types/plaid'

export function SettingsPage() {
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900">Settings</h1>
        <p className="mt-1 text-sm text-gray-500">Linked institutions and preferences.</p>
      </div>
      <LinkedInstitutionsSection />
    </div>
  )
}

function LinkedInstitutionsSection() {
  const [items, setItems] = useState<PlaidItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [disconnecting, setDisconnecting] = useState<string | null>(null)

  const refresh = useCallback(() => {
    return listItems()
      .then((r) => {
        setItems(r.items)
        setError(null)
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    // setState only fires inside the then/catch callbacks — the effect body
    // itself is sync-pure, satisfying react-hooks/set-state-in-effect.
    void refresh()
  }, [refresh])

  const onDisconnect = async (item: PlaidItem) => {
    const label = item.institution_name ?? item.plaid_item_id
    if (!window.confirm(`Disconnect ${label}? Existing accounts and transactions stay visible.`)) {
      return
    }
    setDisconnecting(item.plaid_item_id)
    try {
      await disconnectItem(item.plaid_item_id)
      await refresh()
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setDisconnecting(null)
    }
  }

  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
        <div className="flex items-center gap-2">
          <Plug size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">Linked Institutions</h2>
        </div>
        <Link
          to="/connect"
          className="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
        >
          Connect new
        </Link>
      </div>

      {error && (
        <div className="mx-5 mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="divide-y divide-gray-100">
        {loading && items.length === 0 && (
          <div className="px-5 py-6 text-center text-sm text-gray-400">Loading…</div>
        )}
        {!loading && items.length === 0 && (
          <div className="px-5 py-6 text-center text-sm text-gray-400">
            No linked institutions yet. Use Connect Bank to add one.
          </div>
        )}
        {items.map((it) => (
          <div key={it.id} className="flex items-center gap-4 px-5 py-3">
            <div className="rounded-md bg-gray-50 p-2 text-gray-500">
              <Landmark size={18} />
            </div>
            <div className="flex-1 min-w-0">
              <div className="truncate font-medium text-gray-900">
                {it.institution_name ?? it.plaid_item_id}
              </div>
              <div className="mt-0.5 text-xs text-gray-500">
                {statusSummary(it)}
                {it.last_sync_error ? ` · ${it.last_sync_error}` : ''}
              </div>
            </div>
            <button
              type="button"
              onClick={() => onDisconnect(it)}
              disabled={disconnecting === it.plaid_item_id}
              className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              aria-label={`Disconnect ${it.institution_name ?? it.plaid_item_id}`}
            >
              <Trash2 size={14} />
              {disconnecting === it.plaid_item_id ? 'Disconnecting…' : 'Disconnect'}
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}

function statusSummary(it: PlaidItem): string {
  const status = it.last_sync_status
  switch (status) {
    case 'ok':
      return it.last_synced_at ? `Synced ${formatRelative(it.last_synced_at)}` : 'Synced'
    case 'syncing':
      return 'Syncing…'
    case 'error':
      return 'Last sync failed'
    case 'never':
      return 'Not yet synced'
    default:
      return status
  }
}

function formatRelative(iso: string): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return 'recently'
  const diff = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (diff < 60) return 'just now'
  const mins = Math.floor(diff / 60)
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  return new Date(iso).toLocaleDateString()
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
