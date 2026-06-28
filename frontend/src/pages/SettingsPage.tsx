import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle, Bot, Check, Info, Landmark, Plug, RefreshCw, Trash2, X } from 'lucide-react'
import { TimeAgo } from '../components/TimeAgo'
import {
  disconnectItem,
  dismissSyncError,
  listItems,
  listSyncErrors,
  retrySyncError,
  syncAccounts,
  syncTransactions,
} from '../api/plaid'
import { getHealth } from '../api/health'
import { getUserSettings, updateUserSettings } from '../api/userSettings'
import type { PlaidItem, PlaidSyncError } from '../types/plaid'
import type { UpdateUserSettingsInput, UserSettingsView } from '../types/userSettings'

export function SettingsPage() {
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900">Settings</h1>
        <p className="mt-1 text-sm text-gray-500">AI provider, linked institutions, and preferences.</p>
      </div>
      <AISettingsSection />
      <PriceSettingsSection />
      <LinkedInstitutionsSection />
      <AboutSection />
    </div>
  )
}

// AboutSection surfaces the running build's git SHA (from GET /health) so
// deploy freshness is verifiable from the UI without shell access (#310).
function AboutSection() {
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void getHealth()
      .then((h) => { if (!cancelled) setVersion(h.version ?? 'unknown') })
      .catch(() => { if (!cancelled) setVersion('unknown') })
    return () => { cancelled = true }
  }, [])

  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center gap-2 border-b border-gray-200 px-5 py-3">
        <Info size={16} className="text-gray-500" />
        <h2 className="text-base font-medium text-gray-900">About</h2>
      </div>
      <div className="px-5 py-4">
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500">Build version</span>
          <span className="font-mono text-gray-900">{version ?? '…'}</span>
        </div>
        <p className="mt-1 text-xs text-gray-500">
          Short git SHA of the running server build. Compare against{' '}
          <span className="font-mono">main</span> to confirm the deploy is current.
        </p>
      </div>
    </section>
  )
}

function AISettingsSection() {
  const [settings, setSettings] = useState<UserSettingsView | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [keyDraft, setKeyDraft] = useState('')
  const [ollamaDraft, setOllamaDraft] = useState('')
  const [openaiKeyDraft, setOpenaiKeyDraft] = useState('')
  const [openaiUrlDraft, setOpenaiUrlDraft] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)

  const refresh = useCallback(() => {
    return getUserSettings()
      .then((v) => {
        setSettings(v)
        setOllamaDraft(v.ollama_base_url ?? '')
        setOpenaiUrlDraft(v.openai_base_url ?? '')
        setError(null)
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    // setState only fires inside then/catch/finally — effect body is sync-pure.
    void refresh()
  }, [refresh])

  const save = async (patch: UpdateUserSettingsInput) => {
    setSaving(true)
    setError(null)
    try {
      const v = await updateUserSettings(patch)
      setSettings(v)
      setKeyDraft('')
      setOllamaDraft(v.ollama_base_url ?? '')
      setOpenaiKeyDraft('')
      setOpenaiUrlDraft(v.openai_base_url ?? '')
      setSavedFlash(true)
      window.setTimeout(() => setSavedFlash(false), 1500)
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  if (loading || !settings) {
    return (
      <section className="rounded-lg border border-gray-200 bg-white px-5 py-6 text-sm text-gray-400">
        Loading AI settings…
      </section>
    )
  }

  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
        <div className="flex items-center gap-2">
          <Bot size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">AI Advisor</h2>
        </div>
        {savedFlash && (
          <span className="inline-flex items-center gap-1 text-xs text-emerald-700">
            <Check size={14} /> Saved
          </span>
        )}
      </div>

      {error && (
        <div className="mx-5 mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="space-y-5 px-5 py-4">
        {/* Provider radio */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">Preferred provider</label>
          <div className="flex gap-3 text-sm">
            {(['claude', 'ollama', 'openai'] as const).map((p) => (
              <label key={p} className="inline-flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="provider"
                  value={p}
                  checked={settings.preferred_provider === p}
                  onChange={() => void save({ preferred_provider: p })}
                  disabled={saving}
                />
                <span className="text-gray-700">
                  {p === 'claude' ? 'Claude (cloud)' : p === 'ollama' ? 'Ollama (local)' : 'OpenAI-compatible'}
                </span>
              </label>
            ))}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            Claude streams from Anthropic and needs a key. Ollama runs locally — no key, no data leaves
            your machine. OpenAI-compatible points at any chat-completions endpoint — OpenAI itself, or a
            local proxy fronting another subscription.
          </p>
        </div>

        {/* Claude key */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">Claude API key</label>
          <div className="flex items-center gap-2">
            <input
              type="password"
              placeholder={settings.claude_api_key_set ? '••••••••  (key set)' : 'sk-ant-…'}
              value={keyDraft}
              onChange={(e) => setKeyDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ claude_api_key: keyDraft })}
              disabled={!keyDraft.trim() || saving}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save key
            </button>
            {settings.claude_api_key_set && (
              <button
                type="button"
                onClick={() => {
                  if (window.confirm('Remove the stored Claude key?')) {
                    void save({ clear_claude_api_key: true })
                  }
                }}
                disabled={saving}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <Trash2 size={14} /> Remove
              </button>
            )}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            Stored encrypted at rest. The key is never displayed again after you save it — only a
            "set" indicator. Find your key at{' '}
            <span className="font-mono">console.anthropic.com</span>.
          </p>
        </div>

        {/* Ollama URL */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">Ollama base URL</label>
          <div className="flex items-center gap-2">
            <input
              type="text"
              placeholder="http://localhost:11434"
              value={ollamaDraft}
              onChange={(e) => setOllamaDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ ollama_base_url: ollamaDraft })}
              disabled={saving || ollamaDraft === (settings.ollama_base_url ?? '')}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save URL
            </button>
            {settings.ollama_base_url && (
              <button
                type="button"
                onClick={() => void save({ clear_ollama_url: true })}
                disabled={saving}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <Trash2 size={14} /> Clear
              </button>
            )}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            Leave blank to use the server default (<span className="font-mono">http://localhost:11434</span>).
          </p>
        </div>

        {/* OpenAI-compatible base URL */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">OpenAI-compatible base URL</label>
          <div className="flex items-center gap-2">
            <input
              type="text"
              placeholder="https://api.openai.com/v1"
              value={openaiUrlDraft}
              onChange={(e) => setOpenaiUrlDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ openai_base_url: openaiUrlDraft })}
              disabled={saving || openaiUrlDraft === (settings.openai_base_url ?? '')}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save URL
            </button>
            {settings.openai_base_url && (
              <button
                type="button"
                onClick={() => void save({ clear_openai_url: true })}
                disabled={saving}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <Trash2 size={14} /> Clear
              </button>
            )}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            The <span className="font-mono">/v1</span> root to POST against. Leave blank for public OpenAI;
            set it to a local proxy's address to route through another subscription.
          </p>
        </div>

        {/* OpenAI-compatible key */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">OpenAI-compatible API key</label>
          <div className="flex items-center gap-2">
            <input
              type="password"
              placeholder={settings.openai_api_key_set ? '••••••••  (key set)' : 'sk-… (optional for proxies)'}
              value={openaiKeyDraft}
              onChange={(e) => setOpenaiKeyDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ openai_api_key: openaiKeyDraft })}
              disabled={!openaiKeyDraft.trim() || saving}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save key
            </button>
            {settings.openai_api_key_set && (
              <button
                type="button"
                onClick={() => {
                  if (window.confirm('Remove the stored OpenAI key?')) {
                    void save({ clear_openai_api_key: true })
                  }
                }}
                disabled={saving}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <Trash2 size={14} /> Remove
              </button>
            )}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            Stored encrypted at rest, never displayed again after you save it. Optional — some local
            proxies authenticate by other means and need no key.
          </p>
        </div>
      </div>
    </section>
  )
}

// PriceSettingsSection owns the scheduled price refresh opt-in (#338
// Phase 3). Kept separate from the AI section: it's a different egress
// consent (price providers, not AI providers), per ADR-0014 §3.
function PriceSettingsSection() {
  const [settings, setSettings] = useState<UserSettingsView | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [savedFlash, setSavedFlash] = useState(false)

  useEffect(() => {
    getUserSettings()
      .then((v) => setSettings(v))
      .catch((e: unknown) => setError(errMsg(e)))
  }, [])

  const toggle = async (enabled: boolean) => {
    setSaving(true)
    setError(null)
    try {
      const v = await updateUserSettings({ auto_price_refresh: enabled })
      setSettings(v)
      setSavedFlash(true)
      window.setTimeout(() => setSavedFlash(false), 1500)
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setSaving(false)
    }
  }

  if (!settings) {
    return (
      <section className="rounded-lg border border-gray-200 bg-white px-5 py-6 text-sm text-gray-400">
        {error ?? 'Loading price settings…'}
      </section>
    )
  }

  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
        <div className="flex items-center gap-2">
          <RefreshCw size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">Market Prices</h2>
        </div>
        {savedFlash && (
          <span className="inline-flex items-center gap-1 text-xs text-emerald-700">
            <Check size={14} /> Saved
          </span>
        )}
      </div>

      {error && (
        <div className="mx-5 mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="px-5 py-4">
        <label className="flex cursor-pointer items-start gap-3 text-sm">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={settings.auto_price_refresh}
            onChange={(e) => void toggle(e.target.checked)}
            disabled={saving}
          />
          <span>
            <span className="font-medium text-gray-800">Refresh prices automatically (daily)</span>
            <span className="mt-0.5 block text-xs text-gray-500">
              Once a day, fetch current prices for the assets you hold — crypto via CoinGecko, exchange
              rates via the ECB. Only the list of symbols you hold is sent, never quantities or
              balances. Off by default; the manual “Refresh prices” button on Insights works either
              way.
            </span>
          </span>
        </label>
      </div>
    </section>
  )
}

function LinkedInstitutionsSection() {
  const [items, setItems] = useState<PlaidItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [disconnecting, setDisconnecting] = useState<string | null>(null)
  const [syncing, setSyncing] = useState<string | null>(null)
  const [errorModalItem, setErrorModalItem] = useState<PlaidItem | null>(null)

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

  // Re-sync mirrors the initial connect flow (sync-accounts → sync-transactions)
  // so a user can pull fresh data on demand, not only at link time (#311).
  const onSync = async (item: PlaidItem) => {
    setSyncing(item.plaid_item_id)
    setError(null)
    try {
      await syncAccounts(item.plaid_item_id)
      await syncTransactions(item.plaid_item_id)
      await refresh()
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setSyncing(null)
    }
  }

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
          to="/accounts/add"
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
        {items.map((it) => {
          const errCount = it.unresolved_sync_errors ?? 0
          return (
            <div key={it.id} className="flex items-center gap-4 px-5 py-3">
              <div className="rounded-md bg-gray-50 p-2 text-gray-500">
                <Landmark size={18} />
              </div>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <div className="truncate font-medium text-gray-900">
                    {it.institution_name ?? it.plaid_item_id}
                  </div>
                  {errCount > 0 && (
                    <button
                      type="button"
                      onClick={() => setErrorModalItem(it)}
                      className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800 hover:bg-amber-200"
                      aria-label={`Review ${errCount} sync errors for ${it.institution_name ?? it.plaid_item_id}`}
                    >
                      <AlertTriangle size={12} />
                      {errCount}
                    </button>
                  )}
                </div>
                <div className="mt-0.5 text-xs text-gray-500">
                  {statusSummary(it)}
                  {it.last_sync_error ? ` · ${it.last_sync_error}` : ''}
                </div>
              </div>
              <button
                type="button"
                onClick={() => onSync(it)}
                disabled={syncing === it.plaid_item_id || disconnecting === it.plaid_item_id}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                aria-label={`Sync ${it.institution_name ?? it.plaid_item_id}`}
              >
                <RefreshCw size={14} className={syncing === it.plaid_item_id ? 'animate-spin' : ''} />
                {syncing === it.plaid_item_id ? 'Syncing…' : 'Sync'}
              </button>
              <button
                type="button"
                onClick={() => onDisconnect(it)}
                disabled={disconnecting === it.plaid_item_id || syncing === it.plaid_item_id}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                aria-label={`Disconnect ${it.institution_name ?? it.plaid_item_id}`}
              >
                <Trash2 size={14} />
                {disconnecting === it.plaid_item_id ? 'Disconnecting…' : 'Disconnect'}
              </button>
            </div>
          )
        })}
      </div>
      {errorModalItem && (
        <SyncErrorsModal
          item={errorModalItem}
          onClose={() => {
            setErrorModalItem(null)
            // Refresh the badge count after potential resolutions.
            void refresh()
          }}
        />
      )}
    </section>
  )
}

function SyncErrorsModal({ item, onClose }: { item: PlaidItem; onClose: () => void }) {
  const [errors, setErrors] = useState<PlaidSyncError[]>([])
  const [loading, setLoading] = useState(true)
  const [busyID, setBusyID] = useState<number | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const load = useCallback(() => {
    return listSyncErrors(item.plaid_item_id, 'unresolved')
      .then((r) => {
        setErrors(r.errors)
        setErr(null)
      })
      .catch((e: unknown) => setErr(errMsg(e)))
      .finally(() => setLoading(false))
  }, [item.plaid_item_id])

  useEffect(() => {
    // setState only fires inside then/catch/finally — effect body is sync-pure.
    void load()
  }, [load])

  const reload = useCallback(async () => {
    setLoading(true)
    await load()
  }, [load])

  const onRetry = async (row: PlaidSyncError) => {
    setBusyID(row.id)
    try {
      await retrySyncError(row.id)
      await reload()
    } catch (e) {
      setErr(errMsg(e))
    } finally {
      setBusyID(null)
    }
  }

  const onDismiss = async (row: PlaidSyncError) => {
    setBusyID(row.id)
    try {
      await dismissSyncError(row.id)
      await reload()
    } catch (e) {
      setErr(errMsg(e))
    } finally {
      setBusyID(null)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Sync errors"
      onClick={onClose}
    >
      <div
        className="max-h-[90vh] w-full max-w-3xl overflow-hidden rounded-lg bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
          <div className="flex items-center gap-2">
            <AlertTriangle size={16} className="text-amber-600" />
            <h3 className="text-base font-medium text-gray-900">
              Sync errors — {item.institution_name ?? item.plaid_item_id}
            </h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
          >
            <X size={16} />
          </button>
        </div>
        {err && (
          <div className="mx-5 mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
            {err}
          </div>
        )}
        <div className="max-h-[70vh] overflow-y-auto">
          {loading && (
            <div className="px-5 py-6 text-center text-sm text-gray-400">Loading…</div>
          )}
          {!loading && errors.length === 0 && (
            <div className="px-5 py-6 text-center text-sm text-gray-400">
              All errors resolved.
            </div>
          )}
          {errors.map((row) => (
            <div key={row.id} className="border-b border-gray-100 px-5 py-4 last:border-b-0">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-700">
                      {row.error_code}
                    </span>
                    <span className="text-xs text-gray-500">
                      <TimeAgo when={row.occurred_at} />
                    </span>
                  </div>
                  <p className="mt-1 text-sm text-gray-900">{row.error_message}</p>
                  {row.plaid_transaction_id && (
                    <p className="mt-0.5 font-mono text-xs text-gray-500">
                      txn_id: {row.plaid_transaction_id}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 gap-2">
                  <button
                    type="button"
                    onClick={() => onRetry(row)}
                    disabled={busyID === row.id}
                    className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                  >
                    {busyID === row.id ? '…' : 'Retry'}
                  </button>
                  <button
                    type="button"
                    onClick={() => onDismiss(row)}
                    disabled={busyID === row.id}
                    className="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                  >
                    Dismiss
                  </button>
                </div>
              </div>
              <pre className="mt-2 max-h-48 overflow-auto rounded bg-gray-50 p-2 font-mono text-xs text-gray-800">
                {JSON.stringify(row.raw_payload, null, 2)}
              </pre>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function statusSummary(it: PlaidItem): ReactNode {
  const status = it.last_sync_status
  switch (status) {
    case 'ok':
      return it.last_synced_at ? <>Synced <TimeAgo when={it.last_synced_at} /></> : 'Synced'
    case 'ok_with_errors':
      return it.last_synced_at
        ? <>Synced <TimeAgo when={it.last_synced_at} /> (with errors)</>
        : 'Synced (with errors)'
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

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
