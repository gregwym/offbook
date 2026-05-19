import { useCallback, useEffect, useMemo, useState } from 'react'
import { Pencil, Plus, Target, Trash2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import {
  createSharedBudget,
  deleteSharedBudget,
  listSharedBudgets,
  updateSharedBudget,
} from '../api/households'
import { getBudgetPace } from '../api/householdAggregator'
import { useCategoriesStore } from '../store/categoriesStore'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type { Category } from '../types/category'
import type {
  CreateSharedBudgetInput,
  SharedBudget,
  UpdateSharedBudgetInput,
} from '../types/household'
import type { BudgetPaceItem } from '../types/householdAggregator'

const PERIODS = ['monthly', 'weekly', 'annual'] as const

export function HouseholdBudgetsPage() {
  const { householdId } = useScopeStore()
  const { detail, load: loadDetail } = useHouseholdStore()
  const { categories, fetch: fetchCategories } = useCategoriesStore()

  const [budgets, setBudgets] = useState<SharedBudget[]>([])
  const [pace, setPace] = useState<BudgetPaceItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<SharedBudget | null>(null)

  const reload = useCallback(() => {
    if (householdId == null) return Promise.resolve()
    return Promise.all([listSharedBudgets(householdId), getBudgetPace('current_month')])
      .then(([bs, p]) => {
        setBudgets(bs)
        setPace(p)
        setError(null)
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoading(false))
  }, [householdId])

  useEffect(() => {
    if (householdId != null) {
      void loadDetail(householdId)
      void fetchCategories()
      void reload()
    }
  }, [householdId, loadDetail, fetchCategories, reload])

  const paceByBudget = useMemo(() => {
    const m = new Map<number, BudgetPaceItem>()
    for (const p of pace) m.set(p.budget_id, p)
    return m
  }, [pace])

  const categoryName = useCallback(
    (id: number) => categories.find((c) => c.id === id)?.name ?? `category #${id}`,
    [categories],
  )

  if (householdId == null) {
    return (
      <div className="rounded-lg border border-gray-200 bg-white p-8 text-center">
        <Target size={28} className="mx-auto text-gray-300 mb-2" />
        <h1 className="text-base font-medium text-gray-900">No household yet</h1>
        <p className="text-sm text-gray-500 mt-1">
          Use the scope switcher in the sidebar to create or join a household.
        </p>
      </div>
    )
  }

  // Only owner + contributor can mutate. The backend re-checks this; the
  // UI gate just hides controls that would 403.
  const canMutate = detail?.role === 'owner' || detail?.role === 'contributor'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Shared budgets</h1>
          <p className="mt-1 text-sm text-gray-500">
            Household-wide spending envelopes. Pace shown for the current month.
          </p>
        </div>
        {canMutate && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> New shared budget
          </button>
        )}
      </div>

      {error && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <section className="rounded-lg border border-gray-200 bg-white">
        {loading && budgets.length === 0 && (
          <div className="px-5 py-6 text-center text-sm text-gray-400">Loading…</div>
        )}
        {!loading && budgets.length === 0 && (
          <div className="px-5 py-6 text-center text-sm text-gray-400">
            No shared budgets yet.{' '}
            {canMutate ? 'Create one to start tracking household spending.' : 'Ask an owner or contributor to create one.'}
          </div>
        )}
        <div className="divide-y divide-gray-100">
          {budgets.map((b) => {
            const p = paceByBudget.get(b.id)
            const paceNum = p ? Number.parseFloat(p.pace) : 0
            const overBudget = paceNum >= 1
            const warning = paceNum >= 0.8 && paceNum < 1
            return (
              <div key={b.id} className="flex items-center justify-between gap-4 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-gray-900 truncate">{categoryName(b.category_id)}</span>
                    {!b.is_active && (
                      <span className="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600">paused</span>
                    )}
                  </div>
                  <div className="mt-0.5 text-xs text-gray-500">
                    {b.period} · limit <AmountDisplay amount={b.amount} />
                  </div>
                </div>
                <div className="text-right">
                  {p ? (
                    <>
                      <div
                        className={[
                          'text-sm font-medium',
                          overBudget ? 'text-red-700' : warning ? 'text-amber-700' : 'text-gray-900',
                        ].join(' ')}
                      >
                        <AmountDisplay amount={p.spent} /> <span className="text-gray-400">/</span>{' '}
                        <AmountDisplay amount={p.budget} />
                      </div>
                      <div className="text-[11px] text-gray-500">
                        {Math.round(paceNum * 100)}% of budget
                      </div>
                    </>
                  ) : (
                    <div className="text-xs text-gray-400">no spend yet</div>
                  )}
                </div>
                {canMutate && (
                  <div className="flex shrink-0 gap-1">
                    <button
                      type="button"
                      onClick={() => setEditing(b)}
                      aria-label="Edit"
                      className="rounded p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
                    >
                      <Pencil size={14} />
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        if (!householdId) return
                        if (!window.confirm(`Delete shared budget "${categoryName(b.category_id)}"?`)) return
                        try {
                          await deleteSharedBudget(householdId, b.id)
                          await reload()
                        } catch (e) {
                          setError(errMsg(e))
                        }
                      }}
                      aria-label="Delete"
                      className="rounded p-1 text-gray-500 hover:bg-red-50 hover:text-red-700"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </section>

      {(adding || editing) && (
        <BudgetFormModal
          mode={editing ? 'edit' : 'create'}
          initial={editing}
          categories={categories}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={async (input, id) => {
            if (!householdId) return
            try {
              if (editing && id != null) {
                await updateSharedBudget(householdId, id, input as UpdateSharedBudgetInput)
              } else {
                await createSharedBudget(householdId, input as CreateSharedBudgetInput)
              }
              await reload()
              setAdding(false)
              setEditing(null)
            } catch (e) {
              setError(errMsg(e))
            }
          }}
        />
      )}
    </div>
  )
}

function BudgetFormModal({
  mode,
  initial,
  categories,
  onClose,
  onSubmit,
}: {
  mode: 'create' | 'edit'
  initial: SharedBudget | null
  categories: Category[]
  onClose: () => void
  onSubmit: (input: CreateSharedBudgetInput | UpdateSharedBudgetInput, id?: number) => Promise<void>
}) {
  const [categoryID, setCategoryID] = useState<number>(initial?.category_id ?? categories[0]?.id ?? 0)
  const [period, setPeriod] = useState<(typeof PERIODS)[number]>(initial?.period ?? 'monthly')
  const [amount, setAmount] = useState(initial?.amount ?? '0')
  const [isActive, setIsActive] = useState(initial?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      if (mode === 'edit' && initial) {
        await onSubmit(
          {
            category_id: categoryID,
            period,
            amount,
            is_active: isActive,
          },
          initial.id,
        )
      } else {
        await onSubmit({
          category_id: categoryID,
          period,
          amount,
          is_active: isActive,
        })
      }
    } catch (e) {
      setError(errMsg(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      onClick={onClose}
    >
      <form
        onClick={(e) => e.stopPropagation()}
        onSubmit={submit}
        className="w-full max-w-md rounded-lg bg-white shadow-xl"
      >
        <div className="border-b border-gray-200 px-5 py-3">
          <h3 className="text-base font-medium text-gray-900">
            {mode === 'edit' ? 'Edit shared budget' : 'New shared budget'}
          </h3>
        </div>
        <div className="px-5 py-4 space-y-4">
          <label className="block text-sm">
            <div className="font-medium text-gray-700 mb-1">Category</div>
            <select
              value={categoryID || ''}
              onChange={(e) => setCategoryID(Number.parseInt(e.target.value, 10))}
              required
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
            >
              <option value="" disabled>
                Select a category
              </option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Period</div>
              <select
                value={period}
                onChange={(e) => setPeriod(e.target.value as (typeof PERIODS)[number])}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
              >
                {PERIODS.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </label>
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Amount</div>
              <input
                type="text"
                inputMode="decimal"
                required
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
              />
            </label>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
            Active
          </label>
          {error && (
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{error}</div>
          )}
        </div>
        <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-5 py-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={submitting || !categoryID}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
          >
            {submitting ? 'Saving…' : mode === 'edit' ? 'Save' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  )
}

function errMsg(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
