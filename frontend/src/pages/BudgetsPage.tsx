import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Pencil, Plus, Target, Trash2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { useBudgetsStore } from '../store/budgetsStore'
import { useCategoriesStore } from '../store/categoriesStore'
import type { Category } from '../types/category'
import {
  BUDGET_PERIODS,
  type Budget,
  type BudgetPeriod,
  type BudgetSpend,
  type CreateBudgetInput,
  type UpdateBudgetInput,
} from '../types/budget'

export function BudgetsPage() {
  const { budgets, spendByBudgetID, loading, error, fetch, create, update, remove, clearError } = useBudgetsStore()
  const { categories, fetch: fetchCategories } = useCategoriesStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Budget | null>(null)

  useEffect(() => {
    void fetch()
    void fetchCategories()
  }, [fetch, fetchCategories])

  const categoriesById = useMemo(() => {
    const m = new Map<number, Category>()
    for (const c of categories) m.set(c.id, c)
    return m
  }, [categories])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Budgets</h1>
          <p className="mt-1 text-sm text-gray-500">Per-category spend limits per period. Spending in this period is computed from your transactions.</p>
        </div>
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> New budget
        </button>
      </div>

      {error && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {loading && budgets.length === 0 && (
          <div className="rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-gray-400">Loading…</div>
        )}
        {!loading && budgets.length === 0 && (
          <div className="rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-gray-500">
            <Target size={24} className="mx-auto mb-1 text-gray-300" />
            No budgets yet — create one to start tracking spend.
          </div>
        )}
        {budgets.map((b) => (
          <BudgetCard
            key={b.id}
            budget={b}
            spend={spendByBudgetID[b.id]}
            category={categoriesById.get(b.category_id)}
            onEdit={() => setEditing(b)}
            onDelete={async () => {
              if (window.confirm('Delete this budget?')) await remove(b.id)
            }}
          />
        ))}
      </div>

      {adding && (
        <BudgetFormModal
          mode="create"
          categories={categories}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input as CreateBudgetInput)
            setAdding(false)
          }}
        />
      )}
      {editing && (
        <BudgetFormModal
          mode="edit"
          categories={categories}
          budget={editing}
          onClose={() => setEditing(null)}
          onSubmit={async (input) => {
            await update(editing.id, input as UpdateBudgetInput)
            setEditing(null)
          }}
        />
      )}
    </div>
  )
}

type CardProps = {
  budget: Budget
  spend?: BudgetSpend
  category?: Category
  onEdit: () => void
  onDelete: () => Promise<void>
}

function BudgetCard({ budget, spend, category, onEdit, onDelete }: CardProps) {
  const pct = spend ? Math.min(100, Math.round(spend.pct * 100)) : 0
  const over = !!spend && spend.pct >= 1
  const warning = !!spend && spend.pct >= 0.8 && !over
  const barClass = over ? 'bg-red-500' : warning ? 'bg-amber-500' : 'bg-indigo-500'
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold text-gray-900">{category?.name ?? `#${budget.category_id}`}</h2>
          <p className="mt-0.5 text-xs text-gray-500">
            {budget.period}{budget.rollover ? ' · rollover' : ''}{!budget.is_active ? ' · inactive' : ''}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1 text-gray-400">
          <button type="button" onClick={onEdit} aria-label="Edit" className="rounded p-1 hover:bg-gray-100 hover:text-gray-700"><Pencil size={14} /></button>
          <button type="button" onClick={onDelete} aria-label="Delete" className="rounded p-1 hover:bg-gray-100 hover:text-red-700"><Trash2 size={14} /></button>
        </div>
      </div>
      <div className="mt-3 flex items-baseline justify-between text-sm">
        {spend ? <AmountDisplay amount={spend.spent} currency="USD" /> : <span className="text-gray-400">—</span>}
        <span className="text-xs text-gray-500">of <AmountDisplay amount={budget.amount} currency="USD" /></span>
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-gray-100">
        <div className={['h-full', barClass].join(' ')} style={{ width: `${pct}%` }} />
      </div>
      <div className="mt-1 flex justify-between text-xs">
        <span className={over ? 'text-red-700' : warning ? 'text-amber-700' : 'text-gray-500'}>
          {spend ? `${Math.round(spend.pct * 100)}%` : '—'}
          {over ? ' over' : ''}
        </span>
        <span className="text-gray-500">
          {spend ? <><AmountDisplay amount={spend.remaining} currency="USD" /> remaining</> : ''}
        </span>
      </div>
      {spend && (
        <p className="mt-2 text-xs text-gray-400">
          {spend.period_start.slice(0, 10)} → {spend.period_end.slice(0, 10)}
        </p>
      )}
    </div>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  categories: Category[]
  budget?: Budget
  onClose: () => void
  onSubmit: (input: CreateBudgetInput | UpdateBudgetInput) => Promise<void>
}

function BudgetFormModal({ mode, categories, budget, onClose, onSubmit }: FormProps) {
  const [categoryID, setCategoryID] = useState<number | ''>(budget?.category_id ?? '')
  const [period, setPeriod] = useState<BudgetPeriod>(budget?.period ?? 'monthly')
  const [amount, setAmount] = useState<string>(budget?.amount ?? '0')
  const [rollover, setRollover] = useState<boolean>(budget?.rollover ?? false)
  const [isActive, setIsActive] = useState<boolean>(budget?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [categoryError, setCategoryError] = useState<string | null>(null)
  const [amountError, setAmountError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    setCategoryError(null)
    setAmountError(null)
    if (categoryID === '' || !Number.isInteger(categoryID) || categoryID <= 0) {
      setCategoryError('Pick a category.')
      return
    }
    if (!amount.trim()) {
      setAmountError('Amount is required.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit({
        category_id: Number(categoryID),
        period,
        amount,
        rollover,
        is_active: isActive,
      })
    } catch (err) {
      const { code, message } = extractErr(err)
      if (code === 'DUPLICATE_ACTIVE_BUDGET' || code === 'UNKNOWN_CATEGORY') {
        setCategoryError(message)
      } else if (code === 'INVALID_BUDGET_AMOUNT') {
        setAmountError(message)
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
          {mode === 'create' ? 'New budget' : `Edit budget`}
        </div>
        <div className="space-y-3 px-5 py-4">
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
          <Field label="Category" error={categoryError}>
            <select
              className={inputClass}
              value={categoryID === '' ? '' : String(categoryID)}
              onChange={(e) => setCategoryID(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">— pick one —</option>
              {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Period">
              <select className={inputClass} value={period} onChange={(e) => setPeriod(e.target.value as BudgetPeriod)}>
                {BUDGET_PERIODS.map((p) => <option key={p} value={p}>{p}</option>)}
              </select>
            </Field>
            <Field label="Amount" error={amountError}>
              <input className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-700">
            <input type="checkbox" checked={rollover} onChange={(e) => setRollover(e.target.checked)} />
            Rollover unused balance (UI flag — calc lands later)
          </label>
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

function extractErr(err: unknown): { code: string | null; message: string } {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string; code?: string } } }).response
    if (r?.data?.error) return { code: r.data.code ?? null, message: r.data.error }
  }
  if (err instanceof Error) return { code: null, message: err.message }
  return { code: null, message: 'request failed' }
}
