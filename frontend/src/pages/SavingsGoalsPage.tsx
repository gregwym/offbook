// SavingsGoalsPage — scope-agnostic per v6 IA. Served at both `/savings-goals`
// (personal) and `/h/goals` (household); the active scope swaps the data
// source via useScopedGoals, not the component. Linked accounts and the
// "remaining" figure are personal-only and are hidden in household scope.
// Household mutation is gated on the member's role. See hooks/useScopedGoals.ts.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Coins, Pencil, PiggyBank, Plus, Trash2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import {
  useScopedGoals,
  type ScopedGoalInput,
  type ScopedGoalRow,
} from '../hooks/useScopedGoals'
import { useAccountsStore } from '../store/accountsStore'
import type { Account } from '../types/account'

export function SavingsGoalsPage() {
  const {
    scope,
    rows,
    loading,
    error,
    canMutate,
    householdMissing,
    create,
    update,
    remove,
    contribute,
    clearError,
  } = useScopedGoals()
  const { accounts, fetch: fetchAccounts } = useAccountsStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<ScopedGoalRow | null>(null)
  const [contributing, setContributing] = useState<ScopedGoalRow | null>(null)
  const [rowError, setRowError] = useState<string | null>(null)

  const isHousehold = scope === 'household'

  useEffect(() => {
    // Linked accounts are a personal-scope affordance only.
    if (!isHousehold) void fetchAccounts()
  }, [isHousehold, fetchAccounts])

  const accountsById = useMemo(() => {
    const m = new Map<number, Account>()
    for (const a of accounts) m.set(a.id, a)
    return m
  }, [accounts])

  if (householdMissing) {
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

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">
            {isHousehold ? 'Shared goals' : 'Savings goals'}
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            {isHousehold
              ? 'Household-wide savings targets. Contributions add to a single total.'
              : 'Track progress toward named targets. Contributions are atomic, so logging from multiple devices is safe.'}
          </p>
        </div>
        {canMutate && (
          <button
            type="button"
            onClick={() => setAdding(true)}
            className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          >
            <Plus size={16} /> {isHousehold ? 'New shared goal' : 'New goal'}
          </button>
        )}
      </div>

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

      <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {loading && rows.length === 0 && (
          <div className="rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-gray-400">Loading…</div>
        )}
        {!loading && rows.length === 0 && (
          <div className="rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-gray-500">
            <PiggyBank size={24} className="mx-auto mb-1 text-gray-300" />
            {isHousehold
              ? canMutate
                ? 'No shared goals yet — create one to start tracking household savings.'
                : 'No shared goals yet. Ask an owner or contributor to create one.'
              : 'No goals yet — create one to start tracking.'}
          </div>
        )}
        {rows.map((g) => (
          <GoalCard
            key={g.id}
            goal={g}
            linkedAccount={g.account_id ? accountsById.get(g.account_id) : undefined}
            canMutate={canMutate}
            onEdit={() => setEditing(g)}
            onDelete={async () => {
              if (!window.confirm(`Delete goal "${g.name}"?`)) return
              try {
                await remove(g.id)
              } catch (e) {
                setRowError(errMsg(e))
              }
            }}
            onContribute={() => setContributing(g)}
          />
        ))}
      </div>

      {adding && (
        <GoalFormModal
          mode="create"
          isHousehold={isHousehold}
          accounts={accounts}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input)
            setAdding(false)
          }}
        />
      )}
      {editing && (
        <GoalFormModal
          mode="edit"
          isHousehold={isHousehold}
          goal={editing}
          accounts={accounts}
          onClose={() => setEditing(null)}
          onSubmit={async (input) => {
            await update(editing.id, input)
            setEditing(null)
          }}
        />
      )}
      {contributing && (
        <ContributionModal
          goal={contributing}
          onClose={() => setContributing(null)}
          onSubmit={async (amount) => {
            await contribute(contributing.id, amount)
            setContributing(null)
          }}
        />
      )}
    </div>
  )
}

type CardProps = {
  goal: ScopedGoalRow
  linkedAccount?: Account
  canMutate: boolean
  onEdit: () => void
  onDelete: () => Promise<void>
  onContribute: () => void
}

function GoalCard({ goal, linkedAccount, canMutate, onEdit, onDelete, onContribute }: CardProps) {
  const pct = Math.round(goal.progress_pct * 100)
  const colorClass = pct >= 100 ? 'bg-emerald-500' : pct >= 80 ? 'bg-indigo-500' : 'bg-gray-400'
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4 shadow-sm">
      <div className="flex items-start justify-between">
        <div className="min-w-0">
          <h2 className="truncate text-base font-semibold text-gray-900">{goal.name}</h2>
          {linkedAccount && (
            <p className="mt-0.5 text-xs text-gray-500">linked to {linkedAccount.name}</p>
          )}
        </div>
        {canMutate && (
          <div className="flex shrink-0 items-center gap-1 text-gray-400">
            <button type="button" onClick={onEdit} aria-label="Edit" className="rounded p-1 hover:bg-gray-100 hover:text-gray-700"><Pencil size={14} /></button>
            <button type="button" onClick={onDelete} aria-label="Delete" className="rounded p-1 hover:bg-gray-100 hover:text-red-700"><Trash2 size={14} /></button>
          </div>
        )}
      </div>
      <div className="mt-3 flex items-baseline justify-between text-sm">
        <AmountDisplay amount={goal.current_amount} currency="USD" />
        <span className="text-xs text-gray-500">of <AmountDisplay amount={goal.target_amount} currency="USD" /></span>
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-gray-100">
        <div className={['h-full', colorClass].join(' ')} style={{ width: `${Math.min(100, pct)}%` }} />
      </div>
      <div className="mt-1 flex justify-between text-xs text-gray-500">
        <span>{pct}%</span>
        {goal.remaining != null && (
          <span><AmountDisplay amount={goal.remaining} currency="USD" /> remaining</span>
        )}
      </div>
      {goal.target_date && (
        <p className="mt-2 text-xs text-gray-500">Target: {goal.target_date}</p>
      )}
      {canMutate && (
        <button
          type="button"
          onClick={onContribute}
          className="mt-3 flex w-full items-center justify-center gap-1 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-100"
        >
          <Coins size={14} /> Log contribution
        </button>
      )}
    </div>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  isHousehold: boolean
  goal?: ScopedGoalRow
  accounts: Account[]
  onClose: () => void
  onSubmit: (input: ScopedGoalInput) => Promise<void>
}

function GoalFormModal({ mode, isHousehold, goal, accounts, onClose, onSubmit }: FormProps) {
  const [name, setName] = useState(goal?.name ?? '')
  const [target, setTarget] = useState<string>(goal?.target_amount ?? '0')
  const [targetDate, setTargetDate] = useState<string>(goal?.target_date ?? '')
  const [accountID, setAccountID] = useState<string>(goal?.account_id ? String(goal.account_id) : '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    if (!name.trim()) {
      setError('Name is required.')
      return
    }
    if (!target.trim()) {
      setError('Target amount is required.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit({
        name: name.trim(),
        target_amount: target,
        target_date: targetDate === '' ? null : targetDate,
        account_id: accountID === '' ? null : Number(accountID),
      })
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  const noun = isHousehold ? 'shared goal' : 'goal'

  return (
    <Modal title={mode === 'create' ? `New ${noun}` : `Edit "${goal?.name}"`} onClose={onClose}>
      <div className="space-y-3">
        {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        <Field label="Name">
          <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Target amount">
          <input className={inputClass} value={target} onChange={(e) => setTarget(e.target.value)} inputMode="decimal" />
        </Field>
        <Field label="Target date (optional)">
          <input type="date" className={inputClass} value={targetDate} onChange={(e) => setTargetDate(e.target.value)} />
        </Field>
        {!isHousehold && (
          <Field label="Linked account (optional)">
            <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
              <option value="">(none)</option>
              {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </Field>
        )}
      </div>
      <ModalFooter
        submitting={submitting}
        submitLabel={mode === 'create' ? 'Create' : 'Save'}
        onCancel={onClose}
        onSubmit={submit}
      />
    </Modal>
  )
}

type ContribProps = {
  goal: ScopedGoalRow
  onClose: () => void
  onSubmit: (amount: string) => Promise<void>
}

function ContributionModal({ goal, onClose, onSubmit }: ContribProps) {
  const [amount, setAmount] = useState('0.00')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
    setError(null)
    if (!amount.trim() || amount === '0' || amount === '0.00') {
      setError('Enter a non-zero amount. Use a negative number to record a withdrawal.')
      return
    }
    setSubmitting(true)
    try {
      await onSubmit(amount)
    } catch (err) {
      setError(errMsg(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title={`Contribute to "${goal.name}"`} onClose={onClose}>
      <div className="space-y-3">
        {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
        <Field label="Amount (negative = withdrawal)">
          <input className={inputClass} value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
        </Field>
        <p className="text-xs text-gray-500">Current balance: <AmountDisplay amount={goal.current_amount} currency="USD" /> of <AmountDisplay amount={goal.target_amount} currency="USD" /></p>
      </div>
      <ModalFooter submitting={submitting} submitLabel="Log" onCancel={onClose} onSubmit={submit} />
    </Modal>
  )
}

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-20 flex items-center justify-center bg-black/40 p-4">
      <div className="w-full max-w-lg rounded-lg bg-white shadow-xl">
        <div className="flex items-center justify-between border-b border-gray-200 px-5 py-3 text-lg font-semibold text-gray-900">
          <span>{title}</span>
          <button type="button" onClick={onClose} aria-label="Close" className="text-gray-400 hover:text-gray-700">×</button>
        </div>
        <div className="px-5 py-4">{children}</div>
      </div>
    </div>
  )
}

function ModalFooter({ submitting, submitLabel, onCancel, onSubmit }: { submitting: boolean; submitLabel: string; onCancel: () => void; onSubmit: () => Promise<void> }) {
  return (
    <div className="mt-3 flex justify-end gap-2 border-t border-gray-200 px-5 py-3">
      <button type="button" onClick={onCancel} className="rounded-md border border-gray-300 px-3 py-1.5 text-sm">Cancel</button>
      <button
        type="button"
        onClick={onSubmit}
        disabled={submitting}
        className="rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
      >
        {submitting ? 'Saving…' : submitLabel}
      </button>
    </div>
  )
}

const inputClass = 'w-full rounded border border-gray-300 px-2 py-1 text-sm'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs font-medium text-gray-600">{label}</span>
      {children}
    </label>
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
