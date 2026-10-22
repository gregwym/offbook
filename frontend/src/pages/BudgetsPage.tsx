// BudgetsPage — scope-agnostic per v6 IA. Served at both `/budgets`
// (personal) and `/h/budgets` (household); the active scope swaps the data
// source via useScopedBudgets, not the component. Personal scope always
// allows mutation; household scope gates create/edit/delete on the member's
// role (owner/contributor). See hooks/useScopedBudgets.ts for the branch.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { ChevronLeft, ChevronRight, Pencil, Plus, Target, Trash2 } from 'lucide-react'
import { Link } from 'react-router-dom'
import { AmountDisplay } from '../components/AmountDisplay'
import {
  useScopedBudgets,
  type ScopedBudgetInput,
  type ScopedBudgetRow,
} from '../hooks/useScopedBudgets'
import { useCategoriesStore } from '../store/categoriesStore'
import type { Category } from '../types/category'
import { BUDGET_PERIODS, type BudgetPeriod } from '../types/budget'
import { ModalShell } from '../components/ModalShell'
import { NEW_CATEGORY, NewCategoryDialog, NewCategoryOption } from '../components/NewCategoryDialog'
import { dayBefore, spendingHref } from '../lib/spendingLink'

export function BudgetsPage() {
  const {
    scope,
    month,
    setMonth,
    rows,
    loading,
    error,
    canMutate,
    householdMissing,
    create,
    update,
    remove,
    clearError,
  } = useScopedBudgets()
  const { categories, fetch: fetchCategories } = useCategoriesStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<ScopedBudgetRow | null>(null)
  const [rowError, setRowError] = useState<string | null>(null)

  useEffect(() => {
    void fetchCategories()
  }, [fetchCategories])

  const categoriesById = useMemo(() => {
    const m = new Map<number, Category>()
    for (const c of categories) m.set(c.id, c)
    return m
  }, [categories])

  if (householdMissing) {
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

  const isHousehold = scope === 'household'
  // A past month (#577) shows active monthly budgets only: a weekly or annual
  // period doesn't line up with a calendar month, and a paused budget has no
  // spend to show.
  const shown = month ? rows.filter((r) => r.period === 'monthly' && r.is_active) : rows
  const notShown = rows.length - shown.length

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">
            {isHousehold ? 'Shared budgets' : 'Budgets'}
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            {isHousehold
              ? 'Household-wide spending envelopes. Pace shown for the current month.'
              : 'Per-category spend limits per period. Spending in this period is computed from your transactions.'}
          </p>
        </div>
        {canMutate && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex shrink-0 items-center gap-2 whitespace-nowrap rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> {isHousehold ? 'New shared budget' : 'New budget'}
          </button>
        )}
      </div>

      {!isHousehold && (rows.length > 0 || month) && (
        <MonthStepper month={month} onChange={setMonth} />
      )}
      {/* Each past month shows the limit that applied then (#580). */}
      {month && rows.length > 0 && notShown > 0 && (
        <p className="mt-2 text-xs text-gray-500">
          {`${notShown} weekly, annual or paused ${notShown === 1 ? 'budget is' : 'budgets are'} only shown for the current period.`}
        </p>
      )}

      {(error || rowError) && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error ?? rowError}</span>
          <button
            type="button"
            onClick={() => {
              clearError()
              setRowError(null)
            }}
            className="ml-3 text-red-600 hover:text-red-800"
          >
            ×
          </button>
        </div>
      )}

      <div
        aria-busy={loading}
        className={['mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3', loading && rows.length > 0 ? 'opacity-60' : ''].join(' ')}
      >
        {loading && rows.length === 0 && (
          <div className="rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-gray-400">Loading…</div>
        )}
        {!loading && month && rows.length > 0 && shown.length === 0 && (
          <div className="rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-gray-500">
            No monthly budgets to show for {monthLabel(month)}.
          </div>
        )}
        {!loading && rows.length === 0 && (
          <div className="rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-gray-500">
            <Target size={24} className="mx-auto mb-1 text-gray-300" />
            {isHousehold
              ? canMutate
                ? 'No shared budgets yet — create one to start tracking household spending.'
                : 'No shared budgets yet. Ask an owner or contributor to create one.'
              : 'No budgets yet — create one to start tracking spend.'}
          </div>
        )}
        {shown.map((b) => (
          <BudgetCard
            key={b.id}
            row={b}
            category={categoriesById.get(b.category_id)}
            canMutate={canMutate}
            drillHref={
              // Personal only (#571): a household budget sums members'
              // books, which have no shared transaction list.
              !isHousehold && b.period_start && b.period_end
                ? spendingHref({ start: b.period_start, end: b.period_end, categoryID: b.category_id })
                : undefined
            }
            onEdit={() => setEditing(b)}
            onDelete={async () => {
              if (!window.confirm('Delete this budget?')) return
              try {
                await remove(b.id)
              } catch (e) {
                setRowError(errMsg(e))
              }
            }}
          />
        ))}
      </div>

      {adding && (
        <BudgetFormModal
          mode="create"
          isHousehold={isHousehold}
          categories={categories}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input)
            setAdding(false)
          }}
        />
      )}
      {editing && (
        <BudgetFormModal
          mode="edit"
          isHousehold={isHousehold}
          categories={categories}
          budget={editing}
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

// currentMonth is the server's current month (budget windows are UTC).
function currentMonth(): string {
  return new Date().toISOString().slice(0, 7)
}

function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1 + delta, 1)).toISOString().slice(0, 7)
}

function monthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, 1)).toLocaleString('en-US', { month: 'long', year: 'numeric', timeZone: 'UTC' })
}

// MonthStepper steps the Budgets page back through earlier months (#577).
// month null is the current period; "next" stops there.
function MonthStepper({ month, onChange }: { month: string | null; onChange: (month: string | null) => void }) {
  const shown = month ?? currentMonth()
  const step = (delta: number) => {
    const next = shiftMonth(shown, delta)
    onChange(next >= currentMonth() ? null : next)
  }
  const btn =
    'inline-flex h-10 w-10 items-center justify-center rounded-md border border-gray-300 bg-white text-gray-600 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-40'
  return (
    <nav aria-label="Budget month" className="mt-4 flex flex-wrap items-center gap-2">
      <button type="button" className={btn} onClick={() => step(-1)} aria-label="Previous month">
        <ChevronLeft size={16} />
      </button>
      <span className="min-w-[9rem] text-center text-sm font-medium text-gray-900" aria-live="polite">
        {monthLabel(shown)}
      </span>
      <button type="button" className={btn} onClick={() => step(1)} disabled={month == null} aria-label="Next month">
        <ChevronRight size={16} />
      </button>
      {month && (
        <button type="button" onClick={() => onChange(null)} className="text-sm font-medium text-indigo-600 hover:text-indigo-800">
          This month
        </button>
      )}
    </nav>
  )
}

type CardProps = {
  row: ScopedBudgetRow
  category?: Category
  canMutate: boolean
  // The current period's spending drill-down (#571), when it has one.
  drillHref?: string
  onEdit: () => void
  onDelete: () => Promise<void>
}

function BudgetCard({ row, category, canMutate, drillHref, onEdit, onDelete }: CardProps) {
  const pct = row.pct != null ? Math.min(100, Math.round(row.pct * 100)) : 0
  const over = row.pct != null && row.pct >= 1
  const warning = row.pct != null && row.pct >= 0.8 && !over
  const barClass = over ? 'bg-red-500' : warning ? 'bg-amber-500' : 'bg-indigo-500'
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold text-gray-900">{category?.name ?? `#${row.category_id}`}</h2>
          <p className="mt-0.5 text-xs text-gray-500">
            {row.period}{row.rollover ? ' · rollover' : ''}{!row.is_active ? ' · paused' : ''}
          </p>
        </div>
        {canMutate && (
          <div className="flex shrink-0 items-center gap-1 text-gray-400">
            <button type="button" onClick={onEdit} aria-label="Edit" className="rounded p-1 hover:bg-gray-100 hover:text-gray-700"><Pencil size={14} /></button>
            <button type="button" onClick={onDelete} aria-label="Delete" className="rounded p-1 hover:bg-gray-100 hover:text-red-700"><Trash2 size={14} /></button>
          </div>
        )}
      </div>
      <div className="mt-3 flex items-baseline justify-between text-sm">
        {row.spent != null ? <AmountDisplay amount={row.spent} currency="USD" /> : <span className="text-gray-400">—</span>}
        <span className="text-xs text-gray-500">of <AmountDisplay amount={row.available ?? row.amount} currency="USD" /></span>
      </div>
      {row.rollover && row.carryover != null && Number(row.carryover) > 0 && (
        <p className="mt-1 text-xs text-emerald-700">
          Includes <AmountDisplay amount={row.carryover} currency="USD" /> rolled over
          {Number(row.rollover_start) > 0 ? ' (starting balance plus unspent from earlier periods)' : ' from earlier periods'}
        </p>
      )}
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-gray-100">
        <div className={['h-full', barClass].join(' ')} style={{ width: `${pct}%` }} />
      </div>
      <div className="mt-1 flex justify-between text-xs">
        <span className={over ? 'text-red-700' : warning ? 'text-amber-700' : 'text-gray-500'}>
          {row.pct != null ? `${Math.round(row.pct * 100)}%` : '—'}
          {over ? ' over' : ''}
        </span>
        <span className="text-gray-500">
          {row.remaining == null
            ? ''
            : Number(row.remaining) < 0
              ? <><AmountDisplay amount={row.remaining.replace(/^-/, '')} currency="USD" /> over budget</>
              : <><AmountDisplay amount={row.remaining} currency="USD" /> remaining</>}
        </span>
      </div>
      {row.period_start && row.period_end && (
        <div className="mt-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs">
          <span className="text-gray-400">
            {/* period_end is exclusive; show the last day the period covers. */}
            {row.period_start.slice(0, 10)} → {dayBefore(row.period_end.slice(0, 10))}
          </span>
          {drillHref && (
            <Link to={drillHref} className="font-medium text-indigo-600 hover:text-indigo-800">
              See transactions →
            </Link>
          )}
        </div>
      )}
    </div>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  isHousehold: boolean
  categories: Category[]
  budget?: ScopedBudgetRow
  onClose: () => void
  onSubmit: (input: ScopedBudgetInput) => Promise<void>
}

function BudgetFormModal({ mode, isHousehold, categories, budget, onClose, onSubmit }: FormProps) {
  const [categoryID, setCategoryID] = useState<number | ''>(budget?.category_id ?? '')
  const [period, setPeriod] = useState<BudgetPeriod>(budget?.period ?? 'monthly')
  const [amount, setAmount] = useState<string>(budget?.amount ?? '0')
  const [rollover, setRollover] = useState<boolean>(budget?.rollover ?? false)
  const [rolloverStart, setRolloverStart] = useState<string>(trimZeros(budget?.rollover_start ?? '0'))
  const [startError, setStartError] = useState<string | null>(null)
  const [isActive, setIsActive] = useState<boolean>(budget?.is_active ?? true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [categoryError, setCategoryError] = useState<string | null>(null)
  const [creatingCategory, setCreatingCategory] = useState(false)
  const [amountError, setAmountError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    setCategoryError(null)
    setAmountError(null)
    setStartError(null)
    if (categoryID === '' || !Number.isInteger(categoryID) || categoryID <= 0) {
      setCategoryError('Pick a category.')
      return
    }
    if (!amount.trim()) {
      setAmountError('Amount is required.')
      return
    }
    const start = rolloverStart.trim() === '' ? '0' : rolloverStart.trim()
    if (rollover && !/^\d+(\.\d+)?$/.test(start)) {
      setStartError('Enter a starting balance of 0 or more, like 40.00.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit({
        category_id: Number(categoryID),
        period,
        amount,
        rollover,
        // Kept while rollover is off so re-enabling restores it; the backend
        // ignores it until rollover is on.
        rollover_start: rollover ? start : (budget?.rollover_start ?? '0'),
        is_active: isActive,
      })
    } catch (err) {
      const { code, message } = extractErr(err)
      if (code === 'DUPLICATE_ACTIVE_BUDGET' || code === 'UNKNOWN_CATEGORY') {
        setCategoryError(message)
      } else if (code === 'INVALID_BUDGET_AMOUNT') {
        setAmountError(message)
      } else if (code === 'INVALID_ROLLOVER_START') {
        setStartError(message)
      } else {
        setError(message)
      }
    } finally {
      setSubmitting(false)
    }
  }

  const noun = isHousehold ? 'shared budget' : 'budget'

  return (
    <ModalShell aria-label={mode === 'create' ? `New ${noun}` : `Edit ${noun}`}>
      <div className="border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
        {mode === 'create' ? `New ${noun}` : `Edit ${noun}`}
      </div>
      <div className="space-y-3 px-5 py-4">
        {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        <Field label="Category" error={categoryError}>
          <select
            className={inputClass}
            value={categoryID === '' ? '' : String(categoryID)}
            onChange={(e) => {
              if (e.target.value === NEW_CATEGORY) {
                setCreatingCategory(true)
                return
              }
              setCategoryID(e.target.value === '' ? '' : Number(e.target.value))
            }}
          >
            <option value="">— pick one —</option>
            {/* A shared budget is seen by every member, so it can only use
                built-in categories, not one a member created (#573). */}
            {categories
              .filter((c) => !isHousehold || c.user_id == null)
              .map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            {/* Your own category can't back a shared budget (#573). */}
            {!isHousehold && <NewCategoryOption />}
          </select>
        </Field>
        {creatingCategory && (
          <NewCategoryDialog
            onCreated={(c) => { setCreatingCategory(false); setCategoryID(c.id); setCategoryError(null) }}
            onCancel={() => setCreatingCategory(false)}
          />
        )}
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
        <label className="flex items-start gap-2 text-sm text-gray-700">
          <input type="checkbox" className="mt-1" checked={rollover} onChange={(e) => setRollover(e.target.checked)} />
          <span>
            Roll unspent balance into the next {period === 'monthly' ? 'month' : period === 'weekly' ? 'week' : 'year'}
            <span className="block text-xs text-gray-500">
              {isHousehold
                ? 'Shared budgets are tracked per calendar month; unspent balance carries month to month, and an overspent month resets it to zero.'
                : 'Starts from the balance below in the period you create it; an overspent period resets the balance to zero.'}
            </span>
          </span>
        </label>
        {rollover && (
          <div>
            <Field label="Starting balance" error={startError}>
              <input
                className={inputClass}
                value={rolloverStart}
                onChange={(e) => setRolloverStart(e.target.value)}
                inputMode="decimal"
                placeholder="0.00"
                aria-describedby="rollover-start-help"
              />
            </Field>
            <p id="rollover-start-help" className="mt-1 text-xs text-gray-500">
              Already have money set aside for this category? Enter it to carry it in now.
            </p>
          </div>
        )}
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
    </ModalShell>
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

function errMsg(err: unknown): string {
  return extractErr(err).message
}

// trimZeros renders a NUMERIC(30,18) string ("40.000000000000000000") as a
// short editable value ("40"), without float conversion.
function trimZeros(v: string): string {
  return v.includes('.') ? v.replace(/0+$/, '').replace(/\.$/, '') : v
}
