import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Pencil, PiggyBank, Plus, Trash2 } from 'lucide-react'
import { AmountDisplay } from '../components/AmountDisplay'
import { useAccountsStore } from '../store/accountsStore'
import { useSavingsGoalsStore } from '../store/savingsGoalsStore'
import type { Account } from '../types/account'
import type {
  CreateGoalInput,
  SavingsGoal,
  UpdateGoalInput,
} from '../types/savingsGoal'

export function SavingsGoalsPage() {
  const { goals, loading, error, fetch, create, update, remove, contribute, clearError } = useSavingsGoalsStore()
  const { accounts, fetch: fetchAccounts } = useAccountsStore()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<SavingsGoal | null>(null)
  const [contributing, setContributing] = useState<SavingsGoal | null>(null)

  useEffect(() => {
    void fetch()
    void fetchAccounts()
  }, [fetch, fetchAccounts])

  const accountsById = useMemo(() => {
    const m = new Map<number, Account>()
    for (const a of accounts) m.set(a.id, a)
    return m
  }, [accounts])

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">Savings goals</h1>
          <p className="mt-1 text-sm text-gray-500">Track progress toward named targets. Contributions are atomic, so logging from multiple devices is safe.</p>
        </div>
        <button
          type="button"
          onClick={() => setAdding(true)}
          className="inline-flex items-center gap-2 rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        >
          <Plus size={16} /> New goal
        </button>
      </div>

      {error && (
        <div className="mt-4 flex items-start justify-between rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          <span>{error}</span>
          <button type="button" onClick={clearError} className="ml-3 text-red-600 hover:text-red-800">×</button>
        </div>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {loading && goals.length === 0 && (
          <div className="rounded-lg border border-gray-200 bg-white px-4 py-6 text-center text-gray-400">Loading…</div>
        )}
        {!loading && goals.length === 0 && (
          <div className="rounded-lg border border-dashed border-gray-300 bg-white px-4 py-6 text-center text-gray-500">
            <PiggyBank size={24} className="mx-auto mb-1 text-gray-300" />
            No goals yet — create one to start tracking.
          </div>
        )}
        {goals.map((g) => (
          <GoalCard
            key={g.id}
            goal={g}
            linkedAccount={g.account_id ? accountsById.get(g.account_id) : undefined}
            onEdit={() => setEditing(g)}
            onDelete={async () => {
              if (window.confirm(`Delete goal "${g.name}"?`)) await remove(g.id)
            }}
            onContribute={() => setContributing(g)}
          />
        ))}
      </div>

      {adding && (
        <GoalFormModal
          mode="create"
          accounts={accounts}
          onClose={() => setAdding(false)}
          onSubmit={async (input) => {
            await create(input as CreateGoalInput)
            setAdding(false)
          }}
        />
      )}
      {editing && (
        <GoalFormModal
          mode="edit"
          goal={editing}
          accounts={accounts}
          onClose={() => setEditing(null)}
          onSubmit={async (input) => {
            await update(editing.id, input as UpdateGoalInput)
            setEditing(null)
          }}
        />
      )}
      {contributing && (
        <ContributionModal
          goal={contributing}
          onClose={() => setContributing(null)}
          onSubmit={async (amount) => {
            await contribute(contributing.id, { amount })
            setContributing(null)
          }}
        />
      )}
    </div>
  )
}

type CardProps = {
  goal: SavingsGoal
  linkedAccount?: Account
  onEdit: () => void
  onDelete: () => Promise<void>
  onContribute: () => void
}

function GoalCard({ goal, linkedAccount, onEdit, onDelete, onContribute }: CardProps) {
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
        <div className="flex shrink-0 items-center gap-1 text-gray-400">
          <button type="button" onClick={onEdit} aria-label="Edit" className="rounded p-1 hover:bg-gray-100 hover:text-gray-700"><Pencil size={14} /></button>
          <button type="button" onClick={onDelete} aria-label="Delete" className="rounded p-1 hover:bg-gray-100 hover:text-red-700"><Trash2 size={14} /></button>
        </div>
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
        <span><AmountDisplay amount={goal.remaining} currency="USD" /> remaining</span>
      </div>
      {goal.target_date && (
        <p className="mt-2 text-xs text-gray-500">Target: {goal.target_date}</p>
      )}
      <button
        type="button"
        onClick={onContribute}
        className="mt-3 w-full rounded-md border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-100"
      >
        + Log contribution
      </button>
    </div>
  )
}

type FormProps = {
  mode: 'create' | 'edit'
  goal?: SavingsGoal
  accounts: Account[]
  onClose: () => void
  onSubmit: (input: CreateGoalInput | UpdateGoalInput) => Promise<void>
}

function GoalFormModal({ mode, goal, accounts, onClose, onSubmit }: FormProps) {
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
      if (mode === 'create') {
        await onSubmit({
          name: name.trim(),
          target_amount: target,
          target_date: targetDate || null,
          account_id: accountID === '' ? null : Number(accountID),
        })
      } else {
        // Edit: send sparse patch. Use clear_* flags to null fields.
        const patch: UpdateGoalInput = {
          name: name.trim(),
          target_amount: target,
        }
        if (targetDate === '') {
          patch.clear_target_date = true
        } else {
          patch.target_date = targetDate
        }
        if (accountID === '') {
          patch.clear_account_id = true
        } else {
          patch.account_id = Number(accountID)
        }
        await onSubmit(patch)
      }
    } catch (err) {
      setError(extractErr(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal title={mode === 'create' ? 'New goal' : `Edit "${goal?.name}"`} onClose={onClose}>
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
        <Field label="Linked account (optional)">
          <select className={inputClass} value={accountID} onChange={(e) => setAccountID(e.target.value)}>
            <option value="">(none)</option>
            {accounts.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
        </Field>
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
  goal: SavingsGoal
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
      setError(extractErr(err))
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

function extractErr(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const r = (err as { response?: { data?: { error?: string } } }).response
    if (r?.data?.error) return r.data.error
  }
  if (err instanceof Error) return err.message
  return 'request failed'
}
