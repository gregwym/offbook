import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import type { Category } from '../types/category'
import {
  MATCH_TYPES,
  type ApplyResult,
  type CategorizationRule,
  type CreateRuleInput,
  type MatchType,
  type UpdateRuleInput,
} from '../types/categorizationRule'

export function RulesPage() {
  const { rules, loading, error, fetch, create, update, remove, apply, clearError } = useRulesStore()
  const { categories, fetch: fetchCategories } = useCategoriesStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<CategorizationRule | null>(null)
  const [applying, setApplying] = useState(false)
  const [applyMsg, setApplyMsg] = useState<string | null>(null)

  useEffect(() => {
    void fetch()
    void fetchCategories()
  }, [fetch, fetchCategories])

  const categoriesById = useMemo(() => {
    const m = new Map<number, Category>()
    for (const c of categories) m.set(c.id, c)
    return m
  }, [categories])

  // Next priority = (max existing) + 10, or 10 if no rules yet. Matches the
  // backend's tie-break (priority DESC, id ASC) — new rules outrank old ones
  // by default but the user can edit.
  const nextPriority = useMemo(() => {
    if (rules.length === 0) return 10
    return rules.reduce((m, r) => Math.max(m, r.priority), 0) + 10
  }, [rules])

  const runApply = async () => {
    setApplying(true)
    setApplyMsg(null)
    try {
      const result = await apply()
      setApplyMsg(formatApplyResult(result))
    } catch {
      // store already captured the error; toast it via the error banner
    } finally {
      setApplying(false)
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Categorization rules</h1>
          <p className="mt-1 text-sm text-gray-500">
            Auto-categorize transactions on import. Rules apply in priority order; manual picks always win.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={runApply}
            disabled={applying || rules.length === 0}
            className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            <RefreshCw size={16} className={applying ? 'animate-spin' : ''} />
            {applying ? 'Applying…' : 'Re-apply to all transactions'}
          </button>
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> New rule
          </button>
        </div>
      </div>

      {error && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}
      {applyMsg && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
          <span>{applyMsg}</span>
          <button type="button" onClick={() => setApplyMsg(null)} className="ml-3 text-emerald-700 hover:text-emerald-900">×</button>
        </div>
      )}

      <div className="mt-6 overflow-hidden rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="px-4 py-2 text-left">Priority</th>
              <th className="px-4 py-2 text-left">Pattern</th>
              <th className="px-4 py-2 text-left">Match</th>
              <th className="px-4 py-2 text-left">Category</th>
              <th className="px-4 py-2 text-center">Active</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {loading && rules.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && rules.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-gray-400">No rules yet — create one to start auto-categorizing.</td></tr>
            )}
            {rules.map((r) => (
              <tr key={r.id} className="hover:bg-gray-50">
                <td className="px-4 py-2 font-mono text-gray-700">{r.priority}</td>
                <td className="px-4 py-2 font-mono text-gray-900">{r.pattern}</td>
                <td className="px-4 py-2 text-gray-700">{r.match_type}</td>
                <td className="px-4 py-2 text-gray-700">{categoriesById.get(r.category_id)?.name ?? `#${r.category_id}`}</td>
                <td className="px-4 py-2 text-center">
                  <span className={r.is_active ? 'text-emerald-700' : 'text-gray-400'}>
                    {r.is_active ? 'yes' : 'no'}
                  </span>
                </td>
                <td className="px-4 py-2 text-right">
                  <button
                    type="button"
                    onClick={() => setEditing(r)}
                    className="mr-2 text-gray-500 hover:text-gray-900"
                    aria-label="Edit"
                  >
                    <Pencil size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={async () => {
                      if (window.confirm(`Delete rule "${r.pattern}"?`)) {
                        await remove(r.id)
                      }
                    }}
                    className="text-gray-500 hover:text-red-700"
                    aria-label="Delete"
                  >
                    <Trash2 size={16} />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {adding && (
        <RuleFormModal
          mode="create"
          categories={categories}
          defaults={{ priority: nextPriority }}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input as CreateRuleInput)
            setAdding(false)
          }}
        />
      )}

      {editing && (
        <RuleFormModal
          mode="edit"
          categories={categories}
          rule={editing}
          onClose={() => setEditing(null)}
          onSubmit={async (input) => {
            await update(editing.id, input)
            setEditing(null)
          }}
        />
      )}
    </div>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  categories: Category[]
  rule?: CategorizationRule
  defaults?: { priority?: number }
  onClose: () => void
  onSubmit: (input: CreateRuleInput | UpdateRuleInput) => Promise<void>
}

function RuleFormModal({ mode, categories, rule, defaults, onClose, onSubmit }: FormProps) {
  const [pattern, setPattern] = useState(rule?.pattern ?? '')
  const [matchType, setMatchType] = useState<MatchType>(rule?.match_type ?? 'contains')
  const [categoryID, setCategoryID] = useState<number | ''>(rule?.category_id ?? '')
  const [priority, setPriority] = useState<number>(rule?.priority ?? defaults?.priority ?? 10)
  const [isActive, setIsActive] = useState(rule?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  // We split form errors by field when the backend gives us a code; otherwise
  // generic banner.
  const [error, setError] = useState<string | null>(null)
  const [patternError, setPatternError] = useState<string | null>(null)
  const [categoryError, setCategoryError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    setPatternError(null)
    setCategoryError(null)
    if (!pattern.trim()) {
      setPatternError('Pattern is required.')
      return
    }
    if (categoryID === '' || !Number.isInteger(categoryID) || categoryID <= 0) {
      setCategoryError('Pick a category.')
      return
    }
    if (priority < 0) {
      setError('Priority must be >= 0.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit({
        pattern: pattern.trim(),
        match_type: matchType,
        category_id: Number(categoryID),
        priority,
        is_active: isActive,
      })
    } catch (err) {
      const { code, message } = extractErr(err)
      if (code === 'INVALID_REGEX') {
        setPatternError(message)
      } else if (code === 'UNKNOWN_CATEGORY') {
        setCategoryError(message)
      } else {
        setError(message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-lg rounded-lg bg-white shadow-xl">
        <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          {mode === 'create' ? 'New rule' : `Edit "${rule?.pattern}"`}
        </div>
        <div className="space-y-3 px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Pattern" error={patternError}>
            <input
              className={inputClass}
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder={matchType === 'regex' ? 'e.g. ^AMZN\\s' : 'e.g. WHOLEFDS'}
            />
            <p className="mt-1 text-xs text-gray-500">
              {matchType === 'contains' && 'Case-insensitive substring match on description or merchant.'}
              {matchType === 'exact' && 'Case-insensitive exact match on description or merchant.'}
              {matchType === 'regex' && 'Go regexp; case-sensitive unless you prefix (?i).'}
            </p>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Match type">
              <select className={inputClass} value={matchType} onChange={(e) => setMatchType(e.target.value as MatchType)}>
                {MATCH_TYPES.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field label="Priority">
              <input
                className={inputClass}
                type="number"
                min={0}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
              />
            </Field>
          </div>
          <Field label="Category" error={categoryError}>
            <select
              className={inputClass}
              value={categoryID === '' ? '' : String(categoryID)}
              onChange={(e) => setCategoryID(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">— pick one —</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          </Field>
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
            Active
          </label>
        </div>
        <div className="flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
          <button
            type="button"
            onClick={submit}
            disabled={submitting}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? 'Saving…' : mode === 'create' ? 'Create' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, error, children }: { label: string; error?: string | null; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
      {error && <span className="mt-1 block text-xs text-red-600">{error}</span>}
    </label>
  )
}

function formatApplyResult(r: ApplyResult): string {
  const parts = [`Scanned ${r.scanned}`, `updated ${r.updated}`]
  if (r.skipped_manual > 0) {
    parts.push(`skipped ${r.skipped_manual} manual`)
  }
  return parts.join(', ') + '.'
}

function extractErr(err: unknown): { code: string | null; message: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { code: r.data.code ?? null, message: r.data.error }
  }
  if (err instanceof Error) return { code: null, message: err.message }
  return { code: null, message: 'request failed' }
}
