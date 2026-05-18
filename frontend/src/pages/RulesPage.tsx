import { useEffect, useMemo, useState } from 'react'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { RuleFormModal } from '../components/RuleFormModal'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import type { Category } from '../types/category'
import type {
  ApplyResult,
  CategorizationRule,
  CreateRuleInput,
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

function formatApplyResult(r: ApplyResult): string {
  const parts = [`Scanned ${r.scanned}`, `updated ${r.updated}`]
  if (r.skipped_manual > 0) {
    parts.push(`skipped ${r.skipped_manual} manual`)
  }
  return parts.join(', ') + '.'
}
