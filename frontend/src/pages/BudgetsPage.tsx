// BudgetsPage — scope-agnostic per v6 IA. Served at both `/budgets`
// (personal) and `/h/budgets` (household); the active scope swaps the data
// source via useScopedBudgets, not the component. Personal scope always
// allows mutation; household scope gates create/edit/delete on the member's
// role (owner/contributor). See hooks/useScopedBudgets.ts for the branch.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { ChevronRight, Pencil, Plus, Target, Trash2 } from 'lucide-react'
import { MonthStepper } from '../components/MonthStepper'
import { monthLabel, rangeLabel } from '../lib/month'
import { Link } from 'react-router-dom'
import { AmountDisplay } from '../components/AmountDisplay'
import {
  useScopedBudgets,
  type ScopedBudgetInput,
  type ScopedBudgetRow,
} from '../hooks/useScopedBudgets'
import { useCategoriesStore } from '../store/categoriesStore'
import type { Category } from '../types/category'
import { BUDGET_PERIODS, type BudgetMonthSummary, type BudgetPeriod } from '../types/budget'
import { ModalShell } from '../components/ModalShell'
import { NEW_CATEGORY, NewCategoryDialog, NewCategoryOption } from '../components/NewCategoryDialog'
import { spendingHref } from '../lib/spendingLink'

export function BudgetsPage() {
  const {
    scope,
    month,
    setMonth,
    rows,
    summary,
    summaryFailed,
    loading,
    error,
    canMutate,
    householdMissing,
    create,
    update,
    remove,
    reload,
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
  // This month (#589): weekly and annual budgets run on their own dates, so
  // they sit apart from the month's cards and totals. Shared budgets are all
  // tracked per calendar month and stay together.
  const groupOther = !isHousehold && !month
  const monthCards = groupOther ? shown.filter((r) => r.period === 'monthly') : shown
  const otherCards = groupOther ? shown.filter((r) => r.period !== 'monthly') : []
  const renderCard = (b: ScopedBudgetRow) => (
    <BudgetCard
      key={b.id}
      row={b}
      category={categoriesById.get(b.category_id)}
      includes={b.subcategory_ids.map((id) => categoriesById.get(id)?.name ?? `#${id}`)}
      canMutate={canMutate}
      drillHref={
        // Personal only (#571): a household budget sums members'
        // books, which have no shared transaction list.
        !isHousehold && b.period_start && b.period_end
          ? spendingHref({ start: b.period_start, end: b.period_end, categoryID: b.category_id, subcategoryIDs: b.subcategory_ids })
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
  )

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
        <MonthStepper month={month} onChange={setMonth} label="Budget month" />
      )}
      {/* Each past month shows the limit that applied then (#580). */}
      {month && rows.length > 0 && notShown > 0 && (
        <p className="mt-2 text-xs text-gray-500">
          {`${notShown} weekly, annual or paused ${notShown === 1 ? 'budget is' : 'budgets are'} only shown for the current period.`}
        </p>
      )}

      {summary && (summary.budget_count > 0 || Number(summary.total_spending) !== 0) && (
        <MonthTotals
          summary={summary}
          label={monthLabel(summary.month)}
          categoriesById={categoriesById}
          otherPeriod={new Map(rows.filter((r) => r.is_active && r.period !== 'monthly').map((r) => [r.category_id, r.period]))}
          drill={!isHousehold}
          busy={loading}
        />
      )}
      {summaryFailed && !loading && (
        <p className="mt-4 text-sm text-gray-500">
          The month’s totals didn’t load.{' '}
          <button type="button" onClick={() => void reload()} className="font-medium text-indigo-600 hover:text-indigo-800">
            Try again
          </button>
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
              : 'No budgets yet — set a monthly limit for a category to see how your spending tracks against it.'}
            {canMutate && (
              <div>
                <button
                  type="button"
                  onClick={() => setAdding(true)}
                  className="mt-3 inline-flex min-h-9 items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
                >
                  <Plus size={16} /> {isHousehold ? 'Create the first shared budget' : 'Create your first budget'}
                </button>
              </div>
            )}
          </div>
        )}
        {monthCards.map(renderCard)}
      </div>

      {otherCards.length > 0 && (
        <section aria-labelledby="other-periods" className="mt-8">
          <h2 id="other-periods" className="text-sm font-semibold text-gray-900">Other periods</h2>
          <p className="mt-0.5 text-xs text-gray-500">
            Weekly and annual budgets run on their own dates, so they aren’t part of this month’s totals.
          </p>
          <div className="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {otherCards.map(renderCard)}
          </div>
        </section>
      )}

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

// MonthTotals is the summary strip above the cards (#589): what was
// budgeted, spent against it, left, and spent outside any budget, with that
// last figure broken down by category. Every figure comes from the server
// as an exact decimal; nothing is summed here.
function MonthTotals({ summary, label, categoriesById, otherPeriod, drill, busy }: {
  summary: BudgetMonthSummary
  label: string
  categoriesById: Map<number, Category>
  // Categories whose only active budget is weekly or annual, so they read
  // as "not in a budget" for the month; the breakdown says why.
  otherPeriod: Map<number, BudgetPeriod>
  drill: boolean
  busy: boolean
}) {
  const over = Number(summary.remaining) < 0
  const carry = Number(summary.carryover) > 0
  return (
    <section
      aria-label={`${label} totals`}
      aria-busy={busy}
      className={['mt-4 rounded-lg border border-gray-200 bg-white p-4 shadow-sm', busy ? 'opacity-60' : ''].join(' ')}
    >
      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 md:grid-cols-4">
        <Stat label="Budgeted" testID="total-budgeted">
          <AmountDisplay amount={summary.budgeted} currency="USD" />
          {carry && (
            <span className="block text-xs font-normal text-emerald-700">
              + <AmountDisplay amount={summary.carryover} currency="USD" /> rolled over
            </span>
          )}
        </Stat>
        <Stat label="Spent in budgets" testID="total-spent">
          <AmountDisplay amount={summary.spent_in_budgets} currency="USD" />
        </Stat>
        <Stat label={over ? 'Over budget' : 'Remaining'} testID="total-remaining" tone={over ? 'text-red-700' : undefined}>
          <AmountDisplay amount={summary.remaining.replace(/^-/, '')} currency="USD" />
        </Stat>
        <Stat label="Not in a budget" testID="total-unbudgeted">
          <AmountDisplay amount={summary.not_in_budget} currency="USD" />
        </Stat>
      </dl>
      <p className="mt-3 text-xs text-gray-500">
        All spending in {label}: <AmountDisplay amount={summary.total_spending} currency="USD" />
        {drill && (
          <>
            {' · '}
            <Link
              to={spendingHref({ start: summary.period_start, end: summary.period_end })}
              className="font-medium text-indigo-600 hover:text-indigo-800"
            >
              See transactions
            </Link>
          </>
        )}
      </p>
      {summary.unbudgeted.length > 0 && (
        <details className="group mt-3 border-t border-gray-100 pt-3">
          <summary className="flex min-h-10 cursor-pointer list-none items-center gap-1 text-sm font-medium text-gray-900 [&::-webkit-details-marker]:hidden">
            <ChevronRight size={16} aria-hidden="true" className="text-gray-400 group-open:rotate-90" />
            Where the spending outside budgets went
          </summary>
          <ul className="mt-1 divide-y divide-gray-100">
            {summary.unbudgeted.map((u) => {
              const name = u.category_id == null ? 'Uncategorized' : categoriesById.get(u.category_id)?.name ?? `#${u.category_id}`
              const refund = Number(u.spent) < 0
              const period = u.category_id == null ? undefined : otherPeriod.get(u.category_id)
              return (
                <li key={u.category_id ?? 'none'} className="flex items-center justify-between gap-3 py-2 text-sm">
                  <span className="min-w-0 text-gray-700">
                    <span className="block truncate">{name}</span>
                    {period && <span className="block text-xs text-gray-500">Has a {period} budget</span>}
                  </span>
                  <span className="flex shrink-0 items-center gap-3">
                    <span className="text-gray-900">
                      <AmountDisplay amount={u.spent.replace(/^-/, '')} currency="USD" />
                      {refund && <span className="ml-1 text-xs text-gray-500">net refund</span>}
                    </span>
                    {drill && (
                      <Link
                        to={spendingHref({ start: summary.period_start, end: summary.period_end, categoryID: u.category_id })}
                        aria-label={`See ${name} transactions`}
                        className="inline-flex min-h-10 items-center text-xs font-medium text-indigo-600 hover:text-indigo-800"
                      >
                        See →
                      </Link>
                    )}
                  </span>
                </li>
              )
            })}
          </ul>
        </details>
      )}
    </section>
  )
}

function Stat({ label, testID, tone, children }: { label: string; testID: string; tone?: string; children: ReactNode }) {
  return (
    <div data-testid={testID} className="min-w-0">
      <dt className="text-xs text-gray-500">{label}</dt>
      <dd className={['mt-0.5 text-base font-semibold tabular-nums', tone ?? 'text-gray-900'].join(' ')}>{children}</dd>
    </div>
  )
}

type CardProps = {
  row: ScopedBudgetRow
  category?: Category
  // Names of the subcategories a group budget also counts (#596).
  includes: string[]
  canMutate: boolean
  // The current period's spending drill-down (#571), when it has one.
  drillHref?: string
  onEdit: () => void
  onDelete: () => Promise<void>
}

function BudgetCard({ row, category, includes, canMutate, drillHref, onEdit, onDelete }: CardProps) {
  const pct = row.pct != null ? Math.min(100, Math.round(row.pct * 100)) : 0
  const over = row.pct != null && row.pct >= 1
  const warning = row.pct != null && row.pct >= 0.8 && !over
  const barClass = over ? 'bg-red-500' : warning ? 'bg-amber-500' : 'bg-indigo-500'
  const name = category?.name ?? `#${row.category_id}`
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold text-gray-900">{name}</h2>
          <p className="mt-0.5 text-xs text-gray-500">
            {row.period}{row.rollover ? ' · rollover' : ''}{!row.is_active ? ' · paused' : ''}
          </p>
          {includes.length > 0 && (
            <p className="mt-0.5 text-xs text-gray-500">Includes {includes.join(', ')}</p>
          )}
        </div>
        {canMutate && (
          <div className="flex shrink-0 items-center gap-1 text-gray-400">
            {/* 40px touch targets on phones, 32px on desktop; names say which budget (#630). */}
            <button type="button" onClick={onEdit} aria-label={`Edit ${name} budget`} className="inline-flex h-10 w-10 items-center justify-center rounded hover:bg-gray-100 hover:text-gray-700 sm:h-8 sm:w-8"><Pencil size={16} /></button>
            <button type="button" onClick={onDelete} aria-label={`Delete ${name} budget`} className="inline-flex h-10 w-10 items-center justify-center rounded hover:bg-gray-100 hover:text-red-700 sm:h-8 sm:w-8"><Trash2 size={16} /></button>
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
          {/* The share of the budget used, never "N% over": 60 of 50 is 120%
              used and 20% over, and the right-hand caption already names the
              overspend (#706). */}
          {row.pct != null ? `${Math.round(row.pct * 100)}% used` : '—'}
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
            {/* period_end is exclusive; rangeLabel shows the last day covered. */}
            {rangeLabel(row.period_start, row.period_end)}
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
  const [creatingCategory, setCreatingCategory] = useState<HTMLElement | null>(null)
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
                setCreatingCategory(e.currentTarget)
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
            onCreated={(c) => { setCreatingCategory(null); setCategoryID(c.id); setCategoryError(null) }}
            onCancel={() => setCreatingCategory(null)}
            returnFocusTo={creatingCategory}
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
                : 'You can set a starting balance. An overspent period resets the balance to zero.'}
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
