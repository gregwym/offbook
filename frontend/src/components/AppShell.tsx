import {
  ArrowDownToLine,
  Bot,
  LayoutDashboard,
  PiggyBank,
  Receipt,
  Settings,
  Target,
  TrendingUp,
  Users,
  Wallet,
} from 'lucide-react'
import { useEffect } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { useScopeStore } from '../store/scopeStore'
import { SCOPE_HOUSEHOLD, SCOPE_PERSONAL, type Scope } from '../types/scope'

type NavItem = { to: string; label: string; icon: typeof LayoutDashboard }

// Personal-scope routes — mutually exclusive with HOUSEHOLD_NAV per
// .claude/rules/frontend.md (sidebar renders only the active scope's list).
const PERSONAL_NAV: NavItem[] = [
  { to: '/dashboard',     label: 'Dashboard',     icon: LayoutDashboard },
  { to: '/accounts',      label: 'Accounts',      icon: Wallet },
  { to: '/transactions',  label: 'Transactions',  icon: Receipt },
  { to: '/budgets',       label: 'Budgets',       icon: Target },
  { to: '/savings-goals', label: 'Savings Goals', icon: PiggyBank },
  { to: '/investments',   label: 'Investments',   icon: TrendingUp },
  { to: '/import',        label: 'Import',        icon: ArrowDownToLine },
  { to: '/ai',            label: 'AI Advisor',    icon: Bot },
  { to: '/settings',      label: 'Settings',      icon: Settings },
]

// Household-scope routes — all under /h/* (frontend rule). Only rendered when
// the user has an active membership AND has picked household scope.
const HOUSEHOLD_NAV: NavItem[] = [
  { to: '/h/dashboard', label: 'Dashboard',    icon: LayoutDashboard },
  { to: '/h/budgets',   label: 'Budgets',      icon: Target },
  { to: '/h/goals',     label: 'Goals',        icon: PiggyBank },
  { to: '/h/members',   label: 'Members',      icon: Users },
  { to: '/h/ai',        label: 'AI Advisor',   icon: Bot },
  { to: '/h/settings',  label: 'Settings',     icon: Settings },
]

export function AppShell() {
  const { active, available, hydrated, hydrate, setScope } = useScopeStore()

  useEffect(() => {
    if (!hydrated) {
      void hydrate()
    }
  }, [hydrated, hydrate])

  const items = active === SCOPE_HOUSEHOLD ? HOUSEHOLD_NAV : PERSONAL_NAV
  const canSwitch = available.includes(SCOPE_HOUSEHOLD)

  return (
    <div className="flex h-screen w-screen bg-gray-50">
      <aside className="flex w-60 flex-col border-r border-gray-200 bg-white">
        <div className="px-6 py-5 text-lg font-semibold text-gray-900">offbook</div>
        {canSwitch && <ScopePicker active={active} onChange={setScope} />}
        <nav className="flex-1 space-y-1 px-3 pb-4">
          {items.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                [
                  'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition',
                  isActive
                    ? 'bg-indigo-50 text-indigo-700'
                    : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900',
                ].join(' ')
              }
            >
              <Icon size={18} />
              {label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto">
        <div className="mx-auto max-w-6xl px-8 py-8">
          <Outlet />
        </div>
      </main>
    </div>
  )
}

// ScopePicker is a binary toggle between personal (👤) and household (🏠).
// Hidden when the user has no household membership.
function ScopePicker({ active, onChange }: { active: Scope; onChange: (s: Scope) => void }) {
  const opts: Array<{ key: Scope; emoji: string; label: string }> = [
    { key: SCOPE_PERSONAL,  emoji: '\u{1F464}', label: 'Personal' },
    { key: SCOPE_HOUSEHOLD, emoji: '\u{1F3E0}', label: 'Household' },
  ]
  return (
    <div className="mx-3 mb-3 flex rounded-md border border-gray-200 bg-gray-50 p-1">
      {opts.map((o) => (
        <button
          key={o.key}
          type="button"
          onClick={() => onChange(o.key)}
          aria-pressed={active === o.key}
          className={[
            'flex-1 rounded px-2 py-1 text-xs font-medium transition',
            active === o.key
              ? 'bg-white text-gray-900 shadow-sm'
              : 'text-gray-500 hover:text-gray-700',
          ].join(' ')}
        >
          <span className="mr-1">{o.emoji}</span>
          {o.label}
        </button>
      ))}
    </div>
  )
}
