import { useEffect, useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, Pencil, Plus, RefreshCw, Sparkles, Trash2, Undo2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { RuleFormModal, type RuleFormDefaults } from '../components/RuleFormModal'
import { listCategorizationVerdicts } from '../api/aiCategorizationVerdicts'
import { getLatestApplyRun, undoApplyRun } from '../api/categorizationRules'
import { describeApplyRun, formatApplyResult, formatUndoResult } from '../lib/ruleChanges'
import { useAccountsStore } from '../store/accountsStore'
import { useCategoriesStore } from '../store/categoriesStore'
import { useRulesStore } from '../store/rulesStore'
import type { AICategorizationVerdict } from '../types/aiCategorizationVerdict'
import type { Category } from '../types/category'
import type {
  CategorizationRule,
  CreateRuleInput,
  RuleApplyRun,
} from '../types/categorizationRule'

export function RulesPage() {
  const { rules, loading, error, fetch, create, update, remove, move, apply, clearError } = useRulesStore()
  const { categories, fetch: fetchCategories } = useCategoriesStore()
  const { accounts, fetch: fetchAccounts } = useAccountsStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<CategorizationRule | null>(null)
  const [applying, setApplying] = useState(false)
  const [moving, setMoving] = useState(false)
  const [applyMsg, setApplyMsg] = useState<string | null>(null)
  const [previewed, setPreviewed] = useState(false)
  const [verdicts, setVerdicts] = useState<AICategorizationVerdict[]>([])
  const [promoteSeed, setPromoteSeed] = useState<{ merchantKey: string; defaults: RuleFormDefaults } | null>(null)
  const [promotedKeys, setPromotedKeys] = useState<Set<string>>(new Set())
  // The latest committed re-apply, offered for undo until it is undone (#565).
  const [lastRun, setLastRun] = useState<RuleApplyRun | null>(null)
  const [undoing, setUndoing] = useState(false)
  const [undoError, setUndoError] = useState<string | null>(null)

  useEffect(() => {
    void fetch()
    void fetchCategories()
    void fetchAccounts()
    listCategorizationVerdicts()
      .then(setVerdicts)
      .catch(() => {
        // Best-effort — the AI suggestions panel just stays empty; the core
        // rules table/functionality doesn't depend on it.
      })
    getLatestApplyRun()
      .then(setLastRun)
      .catch(() => {
        // Best-effort — without it the page just doesn't offer Undo.
      })
  }, [fetch, fetchCategories, fetchAccounts])

  const visibleVerdicts = useMemo(
    () => verdicts.filter((v) => !promotedKeys.has(v.merchant_key)),
    [verdicts, promotedKeys],
  )

  const categoriesById = useMemo(() => {
    const m = new Map<number, Category>()
    for (const c of categories) m.set(c.id, c)
    return m
  }, [categories])

  const accountsById = useMemo(() => new Map(accounts.map((a) => [a.id, a])), [accounts])

  // Next priority = (max existing) + 10, or 10 if no rules yet. Matches the
  // backend's tie-break (priority DESC, id ASC) — new rules outrank old ones
  // by default but the user can edit.
  const nextPriority = useMemo(() => {
    if (rules.length === 0) return 10
    return rules.reduce((m, r) => Math.max(m, r.priority), 0) + 10
  }, [rules])

  const runMove = async (id: number, offset: -1 | 1) => {
    setMoving(true)
    try {
      await move(id, offset)
    } finally {
      setMoving(false)
    }
  }

  const runApply = async (dryRun = false) => {
    setApplying(true)
    setApplyMsg(null)
    setPreviewed(false)
    try {
      const result = await apply(undefined, dryRun)
      setApplyMsg(formatApplyResult(result))
      setPreviewed(dryRun)
      if (result.run_id) {
        setUndoError(null)
        setLastRun({
          id: result.run_id,
          scope: 'all',
          rows_changed: result.updated,
          created_at: new Date().toISOString(),
        })
      }
    } catch {
      // store already captured the error; toast it via the error banner
    } finally {
      setApplying(false)
    }
  }

  const runUndo = async () => {
    if (!lastRun) return
    setUndoing(true)
    setUndoError(null)
    try {
      const run = await undoApplyRun(lastRun.id)
      setLastRun(run)
      setApplyMsg(formatUndoResult(run))
      setPreviewed(false)
    } catch (err) {
      if (undoErrorCode(err) === 'RULE_APPLY_NOT_UNDOABLE') {
        // Already undone (another tab) or a newer run exists: show what is
        // actually undoable now.
        setUndoError('That re-apply can no longer be undone: only the latest re-apply can be undone, once.')
        getLatestApplyRun().then(setLastRun).catch(() => setLastRun(null))
      } else {
        setUndoError('Could not undo the last re-apply. Nothing was changed; try again.')
      }
    } finally {
      setUndoing(false)
    }
  }

  const canUndo = lastRun != null && !lastRun.undone_at && lastRun.rows_changed > 0

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Rules</h1>
          <p className="mt-1 text-sm text-gray-500">
            Rules categorize, rename, hide, mark reviewed and tag transactions on import. Rules run top to bottom; the first match wins. Manual picks always win.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {/* Bulk actions only mean something once a rule exists (#627). */}
          {rules.length > 0 && (
            <>
              <button
                type="button"
                onClick={() => runApply(true)}
                disabled={applying || rules.length === 0}
                className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                Preview changes
              </button>
              <button
                type="button"
                onClick={() => runApply(false)}
                disabled={applying || rules.length === 0}
                className="inline-flex items-center gap-2 rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <RefreshCw size={16} className={applying ? 'animate-spin' : ''} />
                {applying ? 'Applying…' : 'Re-apply to all transactions'}
              </button>
            </>
          )}
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
          <span>
            {applyMsg}
            {previewed && (
              <button
                type="button"
                onClick={() => runApply(false)}
                disabled={applying}
                className="ml-3 font-medium underline hover:text-emerald-900 disabled:opacity-50"
              >
                Apply now
              </button>
            )}
          </span>
          <button type="button" aria-label="Dismiss" onClick={() => { setApplyMsg(null); setPreviewed(false) }} className="ml-3 text-emerald-700 hover:text-emerald-900">×</button>
        </div>
      )}
      {undoError && (
        <div role="alert" className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{undoError}</span>
          <button type="button" aria-label="Dismiss" onClick={() => setUndoError(null)} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}
      {canUndo && (
        <div className="mt-4 flex flex-col gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-700 sm:flex-row sm:items-center sm:justify-between">
          <span>
            {describeApplyRun(lastRun)} Undo puts those transactions back as they were; anything you changed since stays.
          </span>
          <button
            type="button"
            onClick={runUndo}
            disabled={undoing || applying}
            className="inline-flex shrink-0 items-center justify-center gap-2 self-start rounded-md border border-gray-300 bg-white px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-50 sm:self-auto"
          >
            <Undo2 size={16} />
            {undoing ? 'Undoing…' : 'Undo re-apply'}
          </button>
        </div>
      )}

      {visibleVerdicts.length > 0 && (
        <div className="mt-6 overflow-x-auto rounded-lg border border-gray-200 bg-white">
          <div className="flex items-center gap-2 border-b border-gray-200 px-4 py-3">
            <Sparkles size={16} className="text-indigo-500" />
            <h2 className="text-sm font-medium text-gray-900">AI category suggestions</h2>
            <span className="text-xs text-gray-400">
              Merchants the AI categorizer has seen — turn a repeat guess into a rule.
            </span>
          </div>
          <ul className="divide-y divide-gray-100">
            {visibleVerdicts.map((v) => (
              <li key={v.id} className="flex items-center justify-between px-4 py-2 text-sm">
                <span className="font-mono text-gray-800">{v.merchant_key}</span>
                <span className="mx-3 flex-1 text-gray-500">
                  → {v.category?.name ?? `#${v.category_id}`}
                  <span className="ml-2 text-xs text-gray-400">{Math.round(v.confidence * 100)}% confident</span>
                </span>
                <button
                  type="button"
                  onClick={() =>
                    setPromoteSeed({
                      merchantKey: v.merchant_key,
                      defaults: {
                        pattern: v.merchant_key,
                        match_type: 'contains',
                        category_id: v.category_id,
                        priority: nextPriority,
                      },
                    })
                  }
                  className="inline-flex items-center gap-1 rounded-md border border-gray-300 bg-white px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
                >
                  <Plus size={14} /> Create rule
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="mt-6 overflow-x-auto rounded-lg border border-gray-200 bg-white">
        <table className="min-w-full divide-y divide-gray-200 text-sm">
          <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wider text-gray-500">
            <tr>
              <th className="px-2 py-2 text-left sm:px-4">
                <span aria-hidden="true">#</span>
                <span className="sr-only">Run order</span>
              </th>
              <th className="px-2 py-2 sm:px-4 text-left">Pattern</th>
              <th className="hidden px-4 py-2 text-left sm:table-cell">Match</th>
              <th className="px-2 py-2 sm:px-4 text-left">Category</th>
              <th className="hidden px-4 py-2 text-center sm:table-cell">Active</th>
              <th className="px-2 py-2 sm:px-4"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {loading && rules.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-gray-400">Loading…</td></tr>
            )}
            {!loading && rules.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-8 text-center">
                <div className="text-gray-700">No rules yet.</div>
                <div className="mt-1 text-sm text-gray-500">
                  A rule categorizes matching transactions for you on every import. You can also make one from any
                  transaction’s “Create rule” action.
                </div>
                <button
                  type="button"
                  onClick={() => setAdding(true)}
                  className="mt-3 inline-flex min-h-9 items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
                >
                  <Plus size={16} /> Create your first rule
                </button>
              </td></tr>
            )}
            {rules.map((r, i) => (
              <tr key={r.id} className="hover:bg-gray-50">
                <td className="px-2 py-2 font-mono text-gray-700 sm:px-4">{i + 1}</td>
                <td className="px-2 py-2 sm:px-4 font-mono text-gray-900">
                  {r.pattern}
                  {!r.is_active && <span className="ml-2 text-xs text-gray-400 sm:hidden">(inactive)</span>}
                  {r.account_id && (
                    <span className="block font-sans text-xs text-gray-500">
                      Only in {accountsById.get(r.account_id)?.name ?? 'a deleted account'}
                    </span>
                  )}
                  {(r.amount_min || r.amount_max) && (
                    <span className="block font-sans text-xs text-gray-500">
                      <RuleAmountCondition
                        min={r.amount_min}
                        max={r.amount_max}
                        currency={r.account_id ? accountsById.get(r.account_id)?.currency : undefined}
                      />
                    </span>
                  )}
                  {r.rename_merchant && (
                    <span className="block font-sans text-xs text-gray-500">Renames to {r.rename_merchant}</span>
                  )}
                  {r.hide_matches && (
                    <span className="block font-sans text-xs text-gray-500">Hides matches</span>
                  )}
                  {r.mark_reviewed && (
                    <span className="block font-sans text-xs text-gray-500">Marks reviewed</span>
                  )}
                  {r.add_tags && r.add_tags.length > 0 && (
                    <span className="block font-sans text-xs text-gray-500">Adds tags: {r.add_tags.join(', ')}</span>
                  )}
                </td>
                <td className="hidden px-4 py-2 text-gray-700 sm:table-cell">{r.match_type}</td>
                <td className="px-2 py-2 text-gray-700 sm:px-4">
                  {r.category_id == null ? (
                    <span className="text-gray-500">Category unchanged</span>
                  ) : (
                    (categoriesById.get(r.category_id)?.name ?? `#${r.category_id}`)
                  )}
                </td>
                <td className="hidden px-4 py-2 text-center sm:table-cell">
                  <span className={r.is_active ? 'text-emerald-700' : 'text-gray-400'}>
                    {r.is_active ? 'yes' : 'no'}
                  </span>
                </td>
                <td className="whitespace-nowrap px-2 py-2 text-right sm:px-4">
                  <button
                    type="button"
                    onClick={() => runMove(r.id, -1)}
                    disabled={moving || i === 0}
                    className="p-1 text-gray-500 hover:text-gray-900 disabled:opacity-30"
                    aria-label={`Move "${r.pattern || `rule ${i + 1}`}" up`}
                    title="Run earlier"
                  >
                    <ArrowUp size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={() => runMove(r.id, 1)}
                    disabled={moving || i === rules.length - 1}
                    className="p-1 text-gray-500 hover:text-gray-900 disabled:opacity-30 sm:mr-1"
                    aria-label={`Move "${r.pattern || `rule ${i + 1}`}" down`}
                    title="Run later"
                  >
                    <ArrowDown size={16} />
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditing(r)}
                    className="p-1 text-gray-500 hover:text-gray-900"
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
                    className="p-1 text-gray-500 hover:text-red-700"
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

      {promoteSeed && (
        <RuleFormModal
          mode="create"
          categories={categories}
          defaults={promoteSeed.defaults}
          onClose={() => setPromoteSeed(null)}
          onSubmit={async (input) => {
            await create(input as CreateRuleInput)
            setPromotedKeys((prev) => new Set(prev).add(promoteSeed.merchantKey))
            setPromoteSeed(null)
          }}
        />
      )}
    </div>
  )
}

// States a rule's amount condition (#550) in words; amounts ignore the sign.
function RuleAmountCondition({ min, max, currency }: { min?: string | null; max?: string | null; currency?: string }) {
  if (min && max) {
    return <>Amount <AmountDisplay amount={min} currency={currency} /> to <AmountDisplay amount={max} currency={currency} /></>
  }
  if (min) return <>Amount <AmountDisplay amount={min} currency={currency} /> or more</>
  return <>Amount up to <AmountDisplay amount={max} currency={currency} /></>
}

function undoErrorCode(err: unknown): string | undefined {
  if (err && typeof err === 'object' && 'response' in err) {
    return (err as { response?: { data?: { code?: string } } }).response?.data?.code
  }
  return undefined
}
