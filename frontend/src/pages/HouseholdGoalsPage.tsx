import { useCallback, useEffect, useMemo, useState } from 'react'
import { Coins, Pencil, PiggyBank, Plus, Trash2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { DateDisplay } from '../components/DateDisplay'
import {
  contributeToSharedGoal,
  createSharedGoal,
  deleteSharedGoal,
  listSharedGoals,
  updateSharedGoal,
} from '../api/households'
import { getGoalProgress } from '../api/householdAggregator'
import { useHouseholdStore } from '../store/householdStore'
import { useScopeStore } from '../store/scopeStore'
import type {
  CreateSharedGoalInput,
  SharedGoal,
  UpdateSharedGoalInput,
} from '../types/household'
import type { GoalProgressItem } from '../types/householdAggregator'

export function HouseholdGoalsPage() {
  const { householdId } = useScopeStore()
  const { detail, load: loadDetail } = useHouseholdStore()

  const [goals, setGoals] = useState<SharedGoal[]>([])
  const [progress, setProgress] = useState<GoalProgressItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<SharedGoal | null>(null)
  const [contributing, setContributing] = useState<SharedGoal | null>(null)

  const reload = useCallback(() => {
    if (householdId == null) return Promise.resolve()
    return Promise.all([listSharedGoals(householdId), getGoalProgress()])
      .then(([gs, p]) => {
        setGoals(gs)
        setProgress(p)
        setError(null)
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoading(false))
  }, [householdId])

  useEffect(() => {
    if (householdId != null) {
      void loadDetail(householdId)
      void reload()
    }
  }, [householdId, loadDetail, reload])

  const progressByGoal = useMemo(() => {
    const m = new Map<number, GoalProgressItem>()
    for (const p of progress) m.set(p.goal_id, p)
    return m
  }, [progress])

  if (householdId == null) {
    return (
      <div className="rounded-lg border border-gray-200 bg-white p-8 text-center">
        <PiggyBank size={28} className="mx-auto text-gray-300 mb-2" />
        <h1 className="text-base font-medium text-gray-900">No household yet</h1>
        <p className="text-sm text-gray-500 mt-1">
          Use the scope switcher in the sidebar to create or join a household.
        </p>
      </div>
    )
  }

  const canMutate = detail?.role === 'owner' || detail?.role === 'contributor'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Shared goals</h1>
          <p className="mt-1 text-sm text-gray-500">
            Household-wide savings targets. Contributions add to a single total.
          </p>
        </div>
        {canMutate && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> New shared goal
          </button>
        )}
      </div>

      {error && (
        <div className="flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {loading && goals.length === 0 && (
          <div className="col-span-full rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-sm text-gray-400">
            Loading…
          </div>
        )}
        {!loading && goals.length === 0 && (
          <div className="col-span-full rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-sm text-gray-500">
            <PiggyBank size={24} className="mx-auto mb-1 text-gray-300" />
            No shared goals yet.{' '}
            {canMutate
              ? 'Create one to start tracking household savings.'
              : 'Ask an owner or contributor to create one.'}
          </div>
        )}
        {goals.map((g) => (
          <GoalCard
            key={g.id}
            goal={g}
            progress={progressByGoal.get(g.id)}
            canMutate={canMutate}
            onEdit={() => setEditing(g)}
            onContribute={() => setContributing(g)}
            onDelete={async () => {
              if (!householdId) return
              if (!window.confirm(`Delete shared goal "${g.name}"?`)) return
              try {
                await deleteSharedGoal(householdId, g.id)
                await reload()
              } catch (e) {
                setError(errMsg(e))
              }
            }}
          />
        ))}
      </div>

      {(adding || editing) && (
        <GoalFormModal
          mode={editing ? 'edit' : 'create'}
          initial={editing}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={async (input, id) => {
            if (!householdId) return
            try {
              if (editing && id != null) {
                await updateSharedGoal(householdId, id, input as UpdateSharedGoalInput)
              } else {
                await createSharedGoal(householdId, input as CreateSharedGoalInput)
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

      {contributing && (
        <ContributeModal
          goal={contributing}
          onClose={() => setContributing(null)}
          onSubmit={async (amount) => {
            if (!householdId) return
            try {
              await contributeToSharedGoal(householdId, contributing.id, amount)
              await reload()
              setContributing(null)
            } catch (e) {
              setError(errMsg(e))
            }
          }}
        />
      )}
    </div>
  )
}

function GoalCard({
  goal,
  progress,
  canMutate,
  onEdit,
  onContribute,
  onDelete,
}: {
  goal: SharedGoal
  progress: GoalProgressItem | undefined
  canMutate: boolean
  onEdit: () => void
  onContribute: () => void
  onDelete: () => void
}) {
  const ratio = progress ? Number.parseFloat(progress.progress) : 0
  const pct = Math.max(0, Math.min(1, ratio)) * 100
  return (
    <div className="rounded-lg border border-gray-200 bg-white px-4 py-3 shadow-sm">
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <div className="truncate font-medium text-gray-900">{goal.name}</div>
          <div className="mt-0.5 text-xs text-gray-500">
            {goal.target_date ? <>By <DateDisplay value={goal.target_date} /></> : 'No deadline'}
          </div>
        </div>
        {canMutate && (
          <div className="flex shrink-0 gap-1">
            <button
              type="button"
              onClick={onEdit}
              aria-label="Edit"
              className="rounded p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
            >
              <Pencil size={14} />
            </button>
            <button
              type="button"
              onClick={onDelete}
              aria-label="Delete"
              className="rounded p-1 text-gray-500 hover:bg-red-50 hover:text-red-700"
            >
              <Trash2 size={14} />
            </button>
          </div>
        )}
      </div>
      <div className="mt-3">
        <div className="flex items-baseline justify-between text-sm">
          <span>
            <AmountDisplay amount={goal.current_amount} /> <span className="text-gray-400">/</span>{' '}
            <AmountDisplay amount={goal.target_amount} />
          </span>
          <span className="text-xs text-gray-500">{Math.round(pct)}%</span>
        </div>
        <div className="mt-1 h-2 w-full overflow-hidden rounded-full bg-gray-100">
          <div className="h-full bg-emerald-500" style={{ width: `${pct}%` }} />
        </div>
      </div>
      {canMutate && (
        <button
          type="button"
          onClick={onContribute}
          className="mt-3 inline-flex w-full items-center justify-center gap-1 rounded-md border border-gray-300 bg-white px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50"
        >
          <Coins size={14} /> Contribute
        </button>
      )}
    </div>
  )
}

function GoalFormModal({
  mode,
  initial,
  onClose,
  onSubmit,
}: {
  mode: 'create' | 'edit'
  initial: SharedGoal | null
  onClose: () => void
  onSubmit: (input: CreateSharedGoalInput | UpdateSharedGoalInput, id?: number) => Promise<void>
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [target, setTarget] = useState(initial?.target_amount ?? '0')
  const [targetDate, setTargetDate] = useState(initial?.target_date ?? '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const payload: CreateSharedGoalInput & UpdateSharedGoalInput = {
        name,
        target_amount: target,
      }
      if (targetDate) payload.target_date = targetDate
      else if (mode === 'edit' && initial?.target_date) payload.clear_target_date = true

      if (mode === 'edit' && initial) {
        await onSubmit(payload as UpdateSharedGoalInput, initial.id)
      } else {
        await onSubmit(payload as CreateSharedGoalInput)
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
            {mode === 'edit' ? 'Edit shared goal' : 'New shared goal'}
          </h3>
        </div>
        <div className="px-5 py-4 space-y-3">
          <label className="block text-sm">
            <div className="font-medium text-gray-700 mb-1">Name</div>
            <input
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
            />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Target amount</div>
              <input
                type="text"
                inputMode="decimal"
                required
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
              />
            </label>
            <label className="block text-sm">
              <div className="font-medium text-gray-700 mb-1">Target date</div>
              <input
                type="date"
                value={targetDate ?? ''}
                onChange={(e) => setTargetDate(e.target.value)}
                className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none"
              />
            </label>
          </div>
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
            disabled={submitting || !name || !target}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
          >
            {submitting ? 'Saving…' : mode === 'edit' ? 'Save' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  )
}

function ContributeModal({
  goal,
  onClose,
  onSubmit,
}: {
  goal: SharedGoal
  onClose: () => void
  onSubmit: (amount: string) => Promise<void>
}) {
  const [amount, setAmount] = useState('0')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      onClick={onClose}
    >
      <form
        onClick={(e) => e.stopPropagation()}
        onSubmit={async (e) => {
          e.preventDefault()
          setError(null)
          setSubmitting(true)
          try {
            await onSubmit(amount)
          } catch (e) {
            setError(errMsg(e))
          } finally {
            setSubmitting(false)
          }
        }}
        className="w-full max-w-md rounded-lg bg-white shadow-xl"
      >
        <div className="border-b border-gray-200 px-5 py-3">
          <h3 className="text-base font-medium text-gray-900">Contribute to {goal.name}</h3>
        </div>
        <div className="px-5 py-4 space-y-3">
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
            <p className="mt-1 text-[11px] text-gray-500">Negative values withdraw from the pool.</p>
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
            disabled={submitting || !amount}
            className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40"
          >
            {submitting ? 'Saving…' : 'Contribute'}
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
