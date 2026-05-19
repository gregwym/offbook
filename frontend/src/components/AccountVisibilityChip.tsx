import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Eye, EyeOff, Lock } from 'lucide-react'
import {
  listAccountShares,
  setAccountShare,
  type AccountShare,
  type AccountShareVisibility,
} from '../api/accountShares'

type Props = {
  accountID: number
  householdID: number
}

const OPTIONS: Array<{ value: AccountShareVisibility; label: string; description: string; icon: React.ComponentType<{ size?: number }> }> = [
  {
    value: 'private',
    label: 'Private',
    description: 'Hidden from the household. Default for new accounts.',
    icon: Lock,
  },
  {
    value: 'balance_only',
    label: 'Balance-only',
    description: "Household sees this account's balance in aggregates. Transactions stay private.",
    icon: EyeOff,
  },
  {
    value: 'balance_and_txns',
    label: 'Balance + transactions',
    description: 'Household sees the balance and every transaction in shared aggregates.',
    icon: Eye,
  },
]

const STYLES: Record<AccountShareVisibility, string> = {
  private: 'bg-gray-100 text-gray-700',
  balance_only: 'bg-amber-100 text-amber-800',
  balance_and_txns: 'bg-emerald-100 text-emerald-800',
}

// AccountVisibilityChip renders the current visibility for an account-household
// pair and lets the account owner change it via a click-to-open dropdown.
// Read happens lazily on mount; PUT is optimistic with rollback on error.
export function AccountVisibilityChip({ accountID, householdID }: Props) {
  const [visibility, setVisibility] = useState<AccountShareVisibility>('private')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    // The /accounts/:id/shares endpoint returns 0 or 1 share row per (account,
    // household) — each user is in at most one household. Filter to our household.
    listAccountShares(accountID)
      .then((rows: AccountShare[]) => {
        const mine = rows.find((r) => r.household_id === householdID)
        setVisibility(mine?.visibility ?? 'private')
      })
      .catch(() => {
        // Ignore — the chip falls back to 'private' (the absent-row default).
      })
      .finally(() => setLoading(false))
  }, [accountID, householdID])

  // Close the dropdown when the user clicks outside.
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const choose = async (next: AccountShareVisibility) => {
    setOpen(false)
    if (next === visibility) return
    const prev = visibility
    setVisibility(next)
    setSaving(true)
    try {
      await setAccountShare(accountID, householdID, next)
    } catch {
      setVisibility(prev)
    } finally {
      setSaving(false)
    }
  }

  const current = OPTIONS.find((o) => o.value === visibility) ?? OPTIONS[0]
  const Icon = current.icon

  return (
    <div ref={rootRef} className="relative inline-block">
      <button
        type="button"
        disabled={loading || saving}
        onClick={() => setOpen((o) => !o)}
        title={current.description}
        className={[
          'inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium transition disabled:opacity-50',
          STYLES[visibility],
        ].join(' ')}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <Icon size={12} />
        {loading ? '…' : current.label}
        <ChevronDown size={12} className="opacity-70" />
      </button>
      {open && (
        <div
          role="listbox"
          className="absolute right-0 z-10 mt-1 w-64 rounded-md border border-gray-200 bg-white p-1 shadow-lg"
        >
          {OPTIONS.map((opt) => {
            const Opt = opt.icon
            const active = opt.value === visibility
            return (
              <button
                key={opt.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => void choose(opt.value)}
                className={[
                  'flex w-full items-start gap-2 rounded px-2 py-2 text-left text-xs',
                  active ? 'bg-indigo-50' : 'hover:bg-gray-50',
                ].join(' ')}
              >
                <Opt size={14} />
                <div className="min-w-0">
                  <div className="font-medium text-gray-900">{opt.label}</div>
                  <div className="text-[11px] text-gray-500">{opt.description}</div>
                </div>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
