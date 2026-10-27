import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { usePlaidLink } from 'react-plaid-link'
import { clearPendingLink, savePendingLink } from '../lib/plaidOAuth'
import { LINKED_INSTITUTIONS_ID } from '../lib/plaidReconnect'
import { AlertTriangle, Bot, Check, Info, Landmark, Pencil, Plug, Plus, RefreshCw, Tag, Trash2, X } from 'lucide-react'
import { TimeAgo } from '../components/TimeAgo'
import {
  createLinkToken,
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
import { getCategoryUsage } from '../api/categories'
import { useCategoriesStore } from '../store/categoriesStore'
import { categoryErr } from '../lib/categoryErr'
import type { Category, CategoryUsage } from '../types/category'
import type { PlaidItem, PlaidSyncError } from '../types/plaid'
import type { AIProvider, UpdateUserSettingsInput, UserSettingsView } from '../types/userSettings'

export function SettingsPage() {
  // "Manage categories" links land on #categories (#581).
  const { hash } = useLocation()
  useEffect(() => {
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView?.()
  }, [hash])
  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900">Settings</h1>
        <p className="mt-1 text-sm text-gray-500">Categories, AI provider, linked institutions, and preferences.</p>
      </div>
      <CategoriesSection />
      <AISettingsSection />
      <PriceSettingsSection />
      <CategorizationSettingsSection />
      <LinkedInstitutionsSection />
      <AboutSection />
    </div>
  )
}

// CategoriesSection lets a user add their own categories, rename them
// (#573), and delete or merge them (#575). Built-in categories stay available and read-only. New categories
// land in the shared store, so Transactions, Rules and Budgets pickers see
// them immediately.
function CategoriesSection() {
  const { categories, loaded, error: loadError, fetch, create, rename, remove } = useCategoriesStore()
  const [name, setName] = useState('')
  const [parentID, setParentID] = useState('')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState<string | null>(null)
  const [flash, setFlash] = useState<string | null>(null)

  useEffect(() => { void fetch() }, [fetch])

  const byID = new Map(categories.map((c) => [c.id, c]))
  const mine = categories.filter((c) => c.user_id != null)
  const builtIn = categories.filter((c) => c.user_id == null)
  // Only top-level categories can be a group; the taxonomy is two levels.
  const groups = categories.filter((c) => c.parent_id == null)

  const showFlash = (msg: string) => {
    setFlash(msg)
    window.setTimeout(() => setFlash(null), 2500)
  }

  const add = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) {
      setAddError('Enter a name for the category.')
      return
    }
    setAdding(true)
    setAddError(null)
    try {
      const c = await create({ name: name.trim(), parent_id: parentID ? Number(parentID) : null })
      setName('')
      setParentID('')
      showFlash(`Added “${c.name}”`)
    } catch (err) {
      setFlash(null)
      setAddError(categoryErr(err, name.trim()))
    } finally {
      setAdding(false)
    }
  }

  return (
    <section id="categories" aria-labelledby="categories-heading" className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
        <div className="flex items-center gap-2">
          <Tag size={16} className="text-gray-500" />
          <h2 id="categories-heading" className="text-base font-medium text-gray-900">Categories</h2>
        </div>
      </div>

      <div className="space-y-4 px-5 py-4">
        <p className="text-sm text-gray-600">
          Add your own categories to sort spending the way you think about it, such as Coffee or Pets.
          They show up wherever you pick a category: transactions, rules and budgets.
        </p>

        <form onSubmit={(e) => void add(e)} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <label className="flex-1 text-sm">
            <span className="mb-1 block font-medium text-gray-700">New category</span>
            <input
              type="text"
              value={name}
              maxLength={60}
              onChange={(e) => { setName(e.target.value); if (addError) setAddError(null) }}
              placeholder="e.g. Coffee"
              aria-invalid={addError ? true : undefined}
              aria-describedby={addError ? 'category-add-error' : undefined}
              className="min-h-10 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            />
          </label>
          <label className="text-sm sm:w-56">
            <span className="mb-1 block font-medium text-gray-700">Group under (optional)</span>
            <select
              value={parentID}
              onChange={(e) => setParentID(e.target.value)}
              className="min-h-10 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm"
            >
              <option value="">None</option>
              {groups.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </label>
          <button
            type="submit"
            disabled={adding}
            className="inline-flex min-h-10 items-center justify-center gap-1 rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            <Plus size={16} /> {adding ? 'Adding…' : 'Add category'}
          </button>
        </form>
        {addError && (
          <p id="category-add-error" role="alert" className="text-sm text-red-700">{addError}</p>
        )}

        <div>
          {/* The note sits by the list, not in the header, so it stays in
              view on a phone after a delete deep in the list. */}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-medium text-gray-900">Your categories</h3>
            {flash && (
              <span role="status" className="inline-flex items-center gap-1 text-xs text-emerald-700">
                <Check size={14} /> {flash}
              </span>
            )}
          </div>
          {!loaded && !loadError && <p className="mt-2 text-sm text-gray-400">Loading categories…</p>}
          {loadError && <p className="mt-2 text-sm text-red-700">Couldn't load categories: {loadError}</p>}
          {loaded && mine.length === 0 && (
            <p className="mt-2 text-sm text-gray-500">
              You haven't added any yet. The {builtIn.length} built-in categories below are always available.
            </p>
          )}
          {mine.length > 0 && (
            <ul className="mt-2 divide-y divide-gray-100 rounded-md border border-gray-200">
              {mine.map((c) => (
                <CategoryRow
                  key={c.id}
                  category={c}
                  parentName={c.parent_id != null ? byID.get(c.parent_id)?.name : undefined}
                  onRename={async (next) => {
                    const r = await rename(c.id, next)
                    showFlash(`Renamed to “${r.name}”`)
                  }}
                  replacements={categories.filter((x) => x.id !== c.id)}
                  onDelete={async (replaceWith) => {
                    const moved = await remove(c.id, replaceWith)
                    const into = replaceWith === 'none' ? undefined : byID.get(replaceWith)?.name
                    const n = moved.transactions + moved.splits
                    const what = n > 0 ? ` and moved ${plural(n, 'transaction')}` : ''
                    showFlash(into ? `Merged “${c.name}” into “${into}”` : `Deleted “${c.name}”${what}`)
                  }}
                />
              ))}
            </ul>
          )}
        </div>

        {builtIn.length > 0 && (
          <details className="text-sm">
            <summary className="cursor-pointer text-gray-700">Built-in categories ({builtIn.length})</summary>
            <p className="mt-2 text-gray-600">{builtIn.map((c) => c.name).join(' · ')}</p>
            <p className="mt-1 text-xs text-gray-500">Built-in categories can't be renamed or deleted.</p>
          </details>
        )}
      </div>
    </section>
  )
}

function plural(n: number, noun: string, nouns = `${noun}s`): string {
  return `${n} ${n === 1 ? noun : nouns}`
}

// usageSentence lists what uses a category, e.g. "2 transactions, 1 rule
// and 1 budget".
function usageSentence(u: CategoryUsage): string {
  const parts = [
    u.transactions > 0 && plural(u.transactions, 'transaction'),
    u.splits > 0 && plural(u.splits, 'split line'),
    u.rules > 0 && plural(u.rules, 'rule'),
    u.budgets > 0 && plural(u.budgets, 'budget'),
    u.subcategories > 0 && plural(u.subcategories, 'subcategory', 'subcategories'),
  ].filter(Boolean) as string[]
  if (parts.length <= 1) return parts.join('')
  return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`
}

function CategoryRow({ category, parentName, onRename, replacements, onDelete }: {
  category: Category
  parentName?: string
  onRename: (name: string) => Promise<void>
  replacements: Category[]
  onDelete: (replaceWith: number | 'none') => Promise<void>
}) {
  const [deleting, setDeleting] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(category.name)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (draft.trim() === category.name) {
      setEditing(false)
      return
    }
    if (!draft.trim()) {
      setError('Enter a name for the category.')
      return
    }
    setSaving(true)
    setError(null)
    try {
      await onRename(draft.trim())
      setEditing(false)
    } catch (err) {
      setError(categoryErr(err, draft.trim()))
    } finally {
      setSaving(false)
    }
  }

  if (editing) {
    return (
      <li className="px-3 py-2">
        <form onSubmit={(e) => void save(e)} className="flex flex-wrap items-center gap-2">
          <input
            type="text"
            value={draft}
            maxLength={60}
            autoFocus
            aria-label={`New name for ${category.name}`}
            onChange={(e) => { setDraft(e.target.value); if (error) setError(null) }}
            onKeyDown={(e) => { if (e.key === 'Escape') { setEditing(false); setDraft(category.name); setError(null) } }}
            className="min-h-9 min-w-0 flex-1 rounded-md border border-gray-300 px-2 py-1 text-sm"
          />
          <button
            type="submit"
            disabled={saving}
            className="min-h-9 rounded-md bg-indigo-600 px-3 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
          <button
            type="button"
            onClick={() => { setEditing(false); setDraft(category.name); setError(null) }}
            className="min-h-9 rounded-md px-3 text-sm text-gray-700 hover:bg-gray-100"
          >
            Cancel
          </button>
        </form>
        {error && <p role="alert" className="mt-1 text-sm text-red-700">{error}</p>}
      </li>
    )
  }

  if (deleting) {
    return (
      <li className="px-3 py-3">
        <DeleteCategoryPanel
          category={category}
          replacements={replacements}
          onCancel={() => setDeleting(false)}
          onDelete={onDelete}
        />
      </li>
    )
  }

  return (
    <li className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
      <span className="min-w-0 break-words">
        <span className="font-medium text-gray-900">{category.name}</span>
        {parentName && <span className="ml-2 text-xs text-gray-500">in {parentName}</span>}
      </span>
      <span className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          onClick={() => { setDraft(category.name); setEditing(true) }}
          aria-label={`Rename ${category.name}`}
          className="inline-flex min-h-9 items-center gap-1 rounded-md px-2 text-sm text-gray-700 hover:bg-gray-100"
        >
          <Pencil size={14} /> Rename
        </button>
        <button
          type="button"
          onClick={() => setDeleting(true)}
          aria-label={`Delete or merge ${category.name}`}
          className="inline-flex min-h-9 items-center gap-1 rounded-md px-2 text-sm text-red-700 hover:bg-red-50"
        >
          <Trash2 size={14} /> Delete
        </button>
      </span>
    </li>
  )
}

// DeleteCategoryPanel confirms deleting a category (#575). It shows what
// uses the category and requires somewhere to move it: another category
// (which merges the two) or Uncategorized.
function DeleteCategoryPanel({ category, replacements, onCancel, onDelete }: {
  category: Category
  replacements: Category[]
  onCancel: () => void
  onDelete: (replaceWith: number | 'none') => Promise<void>
}) {
  const [usage, setUsage] = useState<CategoryUsage | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [target, setTarget] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    getCategoryUsage(category.id)
      .then((u) => {
        if (cancelled) return
        setUsage(u)
        // Nothing to move: Uncategorized is as good a choice as any.
        if (Object.values(u).every((n) => n === 0)) setTarget((t) => t || 'none')
      })
      .catch((err: unknown) => { if (!cancelled) setLoadError(errMsg(err)) })
    return () => { cancelled = true }
  }, [category.id, attempt])

  const into = target && target !== 'none' ? replacements.find((c) => String(c.id) === target) : undefined
  const used = usage ? usageSentence(usage) : ''
  const titleID = `delete-category-${category.id}`

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!target) {
      setError('Choose where to move what uses this category.')
      return
    }
    setSaving(true)
    setError(null)
    try {
      await onDelete(target === 'none' ? 'none' : Number(target))
    } catch (err) {
      const code = (err as { response?: { data?: { code?: string } } })?.response?.data?.code
      setError(code === 'INVALID_REPLACEMENT'
        ? 'That category is no longer available. Pick another one.'
        : categoryErr(err, category.name))
      setSaving(false)
    }
  }

  return (
    <form
      onSubmit={(e) => void submit(e)}
      onKeyDown={(e) => { if (e.key === 'Escape') onCancel() }}
      aria-labelledby={titleID}
      className="space-y-3 text-sm"
    >
      <h4 id={titleID} className="font-medium text-gray-900">Delete “{category.name}”</h4>
      {!usage && !loadError && <p className="text-gray-400">Checking what uses it…</p>}
      {loadError && (
        <p className="text-red-700">
          Couldn't check what uses this category: {loadError}{' '}
          <button
            type="button"
            onClick={() => { setLoadError(null); setAttempt((n) => n + 1) }}
            className="font-medium underline"
          >
            Try again
          </button>
        </p>
      )}
      {usage && (
        <p className="text-gray-700">
          {used ? <>Used by {used}.</> : <>Nothing uses it yet.</>}
        </p>
      )}
      {usage && (
        <label className="block">
          <span className="mb-1 block font-medium text-gray-700">Move everything to</span>
          <select
            value={target}
            autoFocus
            onChange={(e) => { setTarget(e.target.value); if (error) setError(null) }}
            aria-invalid={error ? true : undefined}
            className="min-h-10 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm sm:w-72"
          >
            <option value="">Choose a category…</option>
            <option value="none">Uncategorized</option>
            {replacements.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </label>
      )}
      {usage && used && into && (
        <p className="text-gray-600">
          Everything moves to “{into.name}”, which merges the two.
          {usage.budgets > 0 && ` If “${into.name}” already has a budget for the same period, the two amounts are added together.`}
        </p>
      )}
      {usage && used && target === 'none' && (
        <p className="text-gray-600">
          Transactions and split lines become uncategorized.
          {usage.rules > 0 && ' Rules stop setting a category; a rule with nothing else to do is turned off.'}
          {usage.budgets > 0 && ` Budgets for “${category.name}” are removed.`}
        </p>
      )}
      {usage && usage.subcategories > 0 && target && (
        <p className="text-gray-600">
          {into && (into.parent_id == null || into.parent_id === category.id)
            ? `Its subcategories move under “${into.name}”.`
            : 'Its subcategories become top-level categories.'}
        </p>
      )}
      {error && <p role="alert" className="text-red-700">{error}</p>}
      <div className="flex flex-wrap gap-2">
        <button
          type="submit"
          disabled={saving || !usage}
          className="min-h-10 rounded-md bg-red-600 px-4 font-medium text-white hover:bg-red-700 disabled:opacity-50"
        >
          {saving ? 'Working…' : into ? `Merge into “${into.name}”` : `Delete “${category.name}”`}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="min-h-10 rounded-md px-4 text-gray-700 hover:bg-gray-100"
        >
          Cancel
        </button>
      </div>
    </form>
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

// Per-protocol presentation. Internal values stay claude|ollama|openai for
// wire stability; the UI frames them as protocols.
const PROTOCOL_LABELS: Record<AIProvider, string> = {
  claude: 'Anthropic (Messages)',
  ollama: 'Ollama (local)',
  openai: 'OpenAI (Chat Completions)',
}
const PROTOCOL_ENDPOINT_PLACEHOLDER: Record<AIProvider, string> = {
  claude: 'https://api.anthropic.com/v1/messages',
  ollama: 'http://localhost:11434',
  openai: 'https://api.openai.com/v1',
}
const PROTOCOL_HINTS: Record<AIProvider, string> = {
  claude: 'Anthropic Messages API. Needs a token; leave the endpoint blank for the public API.',
  ollama: 'Local Ollama daemon — no token, no data leaves your machine.',
  openai:
    'Any OpenAI-compatible chat-completions endpoint — OpenAI itself, or a local proxy fronting another subscription.',
}

function AISettingsSection() {
  const [settings, setSettings] = useState<UserSettingsView | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [tokenDraft, setTokenDraft] = useState('')
  const [endpointDraft, setEndpointDraft] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)

  const refresh = useCallback(() => {
    return getUserSettings()
      .then((v) => {
        setSettings(v)
        setEndpointDraft(v.api_endpoint ?? '')
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
      setTokenDraft('')
      setEndpointDraft(v.api_endpoint ?? '')
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
        {/* Protocol picker */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">API protocol</label>
          <div className="flex flex-wrap gap-3 text-sm">
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
                <span className="text-gray-700">{PROTOCOL_LABELS[p]}</span>
              </label>
            ))}
          </div>
          <p className="text-xs text-gray-500 mt-1">{PROTOCOL_HINTS[settings.preferred_provider]}</p>
        </div>

        {/* Endpoint URL */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">Endpoint URL</label>
          <div className="flex items-center gap-2">
            <input
              type="text"
              placeholder={PROTOCOL_ENDPOINT_PLACEHOLDER[settings.preferred_provider]}
              value={endpointDraft}
              onChange={(e) => setEndpointDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ api_endpoint: endpointDraft })}
              disabled={saving || endpointDraft === (settings.api_endpoint ?? '')}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save URL
            </button>
            {settings.api_endpoint && (
              <button
                type="button"
                onClick={() => void save({ clear_api_endpoint: true })}
                disabled={saving}
                className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <Trash2 size={14} /> Clear
              </button>
            )}
          </div>
          <p className="text-xs text-gray-500 mt-1">
            Leave blank to use the protocol default (
            <span className="font-mono">{PROTOCOL_ENDPOINT_PLACEHOLDER[settings.preferred_provider]}</span>).
          </p>
        </div>

        {/* Token */}
        <div>
          <label className="block text-sm font-medium text-gray-800 mb-1">API token</label>
          <div className="flex items-center gap-2">
            <input
              type="password"
              placeholder={settings.api_token_set ? '••••••••  (token set)' : 'sk-…'}
              value={tokenDraft}
              onChange={(e) => setTokenDraft(e.target.value)}
              className="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm focus:border-indigo-500 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void save({ api_token: tokenDraft })}
              disabled={!tokenDraft.trim() || saving}
              className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
            >
              Save token
            </button>
            {settings.api_token_set && (
              <button
                type="button"
                onClick={() => {
                  if (window.confirm('Remove the stored API token?')) {
                    void save({ clear_api_token: true })
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
            Stored encrypted at rest, never displayed again after you save it — only a "set" indicator.
            {settings.preferred_provider === 'ollama'
              ? ' Not needed for local Ollama.'
              : settings.preferred_provider === 'openai'
                ? ' Optional — some local proxies authenticate by other means.'
                : ''}
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

// CategorizationSettingsSection owns the daily AI transaction-categorization
// opt-in (#366, ADR-0022 §8). Same stored-consent shape as
// PriceSettingsSection — a different egress (AI provider, not price
// providers), kept in its own section for the same reason.
function CategorizationSettingsSection() {
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
      const v = await updateUserSettings({ auto_categorize: enabled })
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
        {error ?? 'Loading categorization settings…'}
      </section>
    )
  }

  return (
    <section className="rounded-lg border border-gray-200 bg-white">
      <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3">
        <div className="flex items-center gap-2">
          <Bot size={16} className="text-gray-500" />
          <h2 className="text-base font-medium text-gray-900">AI Categorization</h2>
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
            checked={settings.auto_categorize}
            onChange={(e) => void toggle(e.target.checked)}
            disabled={saving}
          />
          <span>
            <span className="font-medium text-gray-800">Categorize with AI automatically (daily)</span>
            <span className="mt-0.5 block text-xs text-gray-500">
              Once a day, transactions your rules and Plaid couldn't place are sent to your configured
              AI provider — merchant name and amount direction only, never account details or full
              descriptions. AI never overrides a manual pick or a rule. Off by default; low-confidence
              guesses are left for the "Needs review" filter on Transactions rather than committed
              silently.
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

  // Reconnect (#364): a reauth_required item needs a fresh Plaid Link
  // session in "update mode" (link_token scoped to its existing item) before
  // syncing can resume. Mirrors AccountsAddPage's Plaid sub-flow, but scoped
  // to one item at a time here.
  const [reconnectItemID, setReconnectItemID] = useState<string | null>(null)
  const [reconnectToken, setReconnectToken] = useState<string | null>(null)
  // Keyed by item: a failed attempt clears reconnectItemID, so the message
  // must remember which row it belongs to or it never renders (#641).
  const [reconnectError, setReconnectError] = useState<{ itemID: string; message: string } | null>(null)

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

  // The reconnect banner and Accounts pill land here (#641). The page-level
  // hash scroll runs before the sections above finish loading and push this
  // one down, so scroll again once the items are in.
  const { hash } = useLocation()
  useEffect(() => {
    if (!loading && hash === `#${LINKED_INSTITUTIONS_ID}`) {
      document.getElementById(LINKED_INSTITUTIONS_ID)?.scrollIntoView?.({ block: 'start' })
    }
  }, [loading, hash])

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

  const onReconnectSuccess = useCallback(() => {
    // Update-mode Link doesn't need a public_token exchange — the item's
    // access_token is unchanged. Resuming a sync is what actually clears
    // reauth_required once the credentials verify.
    const itemID = reconnectItemID
    clearPendingLink()
    setReconnectItemID(null)
    setReconnectToken(null)
    if (!itemID) return
    setSyncing(itemID)
    syncAccounts(itemID)
      .then(() => syncTransactions(itemID))
      .then(() => refresh())
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setSyncing(null))
  }, [reconnectItemID, refresh])

  const onReconnectExit = useCallback(() => {
    clearPendingLink()
    setReconnectItemID(null)
    setReconnectToken(null)
  }, [])

  const { open: openReconnectLink, ready: reconnectLinkReady } = usePlaidLink({
    token: reconnectToken,
    onSuccess: onReconnectSuccess,
    onExit: onReconnectExit,
  })

  useEffect(() => {
    if (reconnectToken && reconnectLinkReady) {
      openReconnectLink()
    }
  }, [reconnectToken, reconnectLinkReady, openReconnectLink])

  const onReconnect = async (item: PlaidItem) => {
    setReconnectError(null)
    setReconnectItemID(item.plaid_item_id)
    try {
      const t = await createLinkToken(item.plaid_item_id)
      // OAuth banks resume on /plaid/oauth-return after a full-page
      // redirect (#502) — stash what that page needs to finish re-auth.
      savePendingLink({
        mode: 'update',
        linkToken: t.link_token,
        expiration: t.expiration,
        itemId: item.plaid_item_id,
      })
      setReconnectToken(t.link_token)
    } catch (e) {
      setReconnectError({ itemID: item.plaid_item_id, message: errMsg(e) })
      setReconnectItemID(null)
    }
  }

  return (
    <section id={LINKED_INSTITUTIONS_ID} className="scroll-mt-16 rounded-lg border border-gray-200 bg-white md:scroll-mt-4">
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
          const needsReconnect = it.last_sync_status === 'reauth_required'
          const label = it.institution_name ?? it.plaid_item_id
          return (
            <div key={it.id} className={needsReconnect ? 'bg-amber-50/50' : undefined}>
              <div className="flex items-center gap-4 px-5 py-3">
                <div className="rounded-md bg-gray-50 p-2 text-gray-500">
                  <Landmark size={18} />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <div className="truncate font-medium text-gray-900">{label}</div>
                    {errCount > 0 && (
                      <button
                        type="button"
                        onClick={() => setErrorModalItem(it)}
                        className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-800 hover:bg-amber-200"
                        aria-label={`Review ${errCount} sync errors for ${label}`}
                      >
                        <AlertTriangle size={12} />
                        {errCount}
                      </button>
                    )}
                  </div>
                  <div className="mt-0.5 text-xs text-gray-500">
                    {statusSummary(it)}
                    {/* A reauth row's error is the raw Plaid code
                        (ITEM_LOGIN_REQUIRED: …) — the summary already says
                        what to do, so only other statuses show it (#641). */}
                    {it.last_sync_error && !needsReconnect ? ` · ${it.last_sync_error}` : ''}
                  </div>
                </div>
                {needsReconnect ? (
                  <button
                    type="button"
                    onClick={() => void onReconnect(it)}
                    disabled={reconnectItemID === it.plaid_item_id || disconnecting === it.plaid_item_id}
                    className="inline-flex items-center gap-1 rounded-md bg-amber-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-amber-700 disabled:opacity-50"
                    aria-label={`Reconnect ${label}`}
                  >
                    <AlertTriangle size={14} />
                    {reconnectItemID === it.plaid_item_id ? 'Reconnecting…' : 'Reconnect'}
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => onSync(it)}
                    disabled={syncing === it.plaid_item_id || disconnecting === it.plaid_item_id}
                    className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                    aria-label={`Sync ${label}`}
                  >
                    <RefreshCw size={14} className={syncing === it.plaid_item_id ? 'animate-spin' : ''} />
                    {syncing === it.plaid_item_id ? 'Syncing…' : 'Sync'}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => onDisconnect(it)}
                  disabled={disconnecting === it.plaid_item_id || syncing === it.plaid_item_id}
                  className="inline-flex items-center gap-1 rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                  aria-label={`Disconnect ${label}`}
                >
                  <Trash2 size={14} />
                  {disconnecting === it.plaid_item_id ? 'Disconnecting…' : 'Disconnect'}
                </button>
              </div>
              {needsReconnect && reconnectError?.itemID === it.plaid_item_id && (
                <div role="alert" className="mx-5 mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
                  Couldn’t start the reconnect: {reconnectError.message}
                </div>
              )}
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
    case 'reauth_required':
      return it.last_synced_at ? (
        <>Your bank needs you to sign in again · last synced <TimeAgo when={it.last_synced_at} /></>
      ) : 'Your bank needs you to sign in again'
    case 'never':
      return 'Not yet synced'
    default:
      return status
  }
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.code === 'PLAID_NOT_CONFIGURED') {
      return 'Plaid is not configured on this instance. Ask the admin to set PLAID_CLIENT_ID and PLAID_SECRET.'
    }
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
