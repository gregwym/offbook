import {
  ArrowDownToLine,
  Bot,
  Home,
  Landmark,
  LayoutDashboard,
  LogOut,
  Menu,
  PiggyBank,
  Receipt,
  Settings,
  Sparkles,
  Target,
  TrendingUp,
  Users,
  Wallet,
  X,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { HouseholdJoinModal } from './HouseholdJoinModal'
import { useAuthStore } from '../store/authStore'
import { useScopeStore } from '../store/scopeStore'
import { SCOPE_HOUSEHOLD, SCOPE_PERSONAL, type Scope } from '../types/scope'

type NavItem = { to: string; label: string; icon: typeof LayoutDashboard }

// Personal-scope routes — mutually exclusive with HOUSEHOLD_NAV per
// .claude/rules/frontend.md (sidebar renders only the active scope's list).
const PERSONAL_NAV: NavItem[] = [
  { to: '/insights',      label: 'Insights',      icon: LayoutDashboard },
  { to: '/accounts',      label: 'Accounts',      icon: Wallet },
  { to: '/connect',       label: 'Connect Bank',  icon: Landmark },
  { to: '/transactions',  label: 'Transactions',  icon: Receipt },
  { to: '/rules',         label: 'Rules',         icon: Sparkles },
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
  { to: '/h/insights',  label: 'Insights',     icon: LayoutDashboard },
  { to: '/h/budgets',   label: 'Budgets',      icon: Target },
  { to: '/h/goals',     label: 'Goals',        icon: PiggyBank },
  { to: '/h/members',   label: 'Members',      icon: Users },
  { to: '/h/ai',        label: 'AI Advisor',   icon: Bot },
  { to: '/h/settings',  label: 'Settings',     icon: Settings },
]

export function AppShell() {
  const { active, available, hydrated, hydrate, setScope } = useScopeStore()
  const signout = useAuthStore((s) => s.signout)
  const navigate = useNavigate()

  useEffect(() => {
    if (!hydrated) {
      void hydrate()
    }
  }, [hydrated, hydrate])

  const items = active === SCOPE_HOUSEHOLD ? HOUSEHOLD_NAV : PERSONAL_NAV
  const canSwitch = available.includes(SCOPE_HOUSEHOLD)
  const [joinOpen, setJoinOpen] = useState(false)

  // Mobile drawer: below md (768px) the sidebar lives behind a hamburger
  // toggle. md+ ignores this entirely — the sidebar stays in the static
  // layout. The drawer closes itself when the user taps a nav link (each
  // NavLink calls closeDrawer onClick) so we don't need a route-change
  // effect — those are flagged by react-hooks/set-state-in-effect anyway.
  const [drawerOpen, setDrawerOpen] = useState(false)
  const closeDrawer = () => setDrawerOpen(false)

  const handleSignout = async () => {
    await signout()
    navigate('/signin', { replace: true })
  }

  const sidebar = (
    <>
      <div className="flex items-center justify-between px-6 py-5">
        <span className="text-lg font-semibold text-gray-900">offbook</span>
        {/* Close button only visible inside the mobile drawer. */}
        <button
          type="button"
          onClick={() => setDrawerOpen(false)}
          aria-label="Close menu"
          className="-mr-2 inline-flex h-11 w-11 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-gray-900 md:hidden"
        >
          <X size={20} />
        </button>
      </div>
      {canSwitch ? (
        <ScopePicker active={active} onChange={setScope} />
      ) : (
        hydrated && <JoinCTA onClick={() => setJoinOpen(true)} />
      )}
      <nav className="flex-1 space-y-1 px-3 pb-4 overflow-y-auto">
        {items.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            onClick={closeDrawer}
            className={({ isActive }) =>
              [
                // min-h-[44px] enforces the iOS HIG tap-target on mobile.
                // md:min-h-0 hands the height back to py-2 on desktop so
                // the sidebar isn't extra tall.
                'flex min-h-[44px] items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition md:min-h-0',
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
      <div className="border-t border-gray-200 p-3">
        <button
          type="button"
          onClick={() => void handleSignout()}
          className="flex min-h-[44px] w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100 hover:text-gray-900 md:min-h-0"
        >
          <LogOut size={16} />
          Sign out
        </button>
      </div>
    </>
  )

  return (
    <div className="flex h-screen w-screen bg-gray-50">
      {/* Desktop sidebar (md+). Always visible, takes its slot in the flex row. */}
      <aside className="hidden w-60 flex-col border-r border-gray-200 bg-white md:flex">
        {sidebar}
      </aside>

      {/* Mobile drawer (< md). Conditionally rendered overlay + slide-in panel. */}
      {drawerOpen && (
        <>
          <div
            onClick={() => setDrawerOpen(false)}
            aria-hidden="true"
            className="fixed inset-0 z-40 bg-black/40 md:hidden"
          />
          <aside
            role="dialog"
            aria-modal="true"
            aria-label="Navigation"
            className="fixed inset-y-0 left-0 z-50 flex w-64 max-w-[80vw] flex-col border-r border-gray-200 bg-white shadow-xl md:hidden"
          >
            {sidebar}
          </aside>
        </>
      )}

      <main className="flex-1 overflow-auto">
        {/* Mobile top bar with hamburger + brand. Hidden md+ since the
            sidebar already shows the brand. */}
        <div className="sticky top-0 z-30 flex items-center gap-2 border-b border-gray-200 bg-white px-3 py-2 md:hidden">
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            aria-label="Open menu"
            aria-expanded={drawerOpen}
            className="inline-flex h-11 w-11 items-center justify-center rounded-md text-gray-700 hover:bg-gray-100"
          >
            <Menu size={22} />
          </button>
          <span className="text-base font-semibold text-gray-900">offbook</span>
        </div>
        {/* Tighter horizontal padding on mobile so narrow viewports actually
            get usable width. The mx-auto + max-w-6xl still caps wide layouts. */}
        <div className="mx-auto max-w-6xl px-4 py-4 md:px-8 md:py-8">
          <Outlet />
        </div>
      </main>
      {joinOpen && <HouseholdJoinModal onClose={() => setJoinOpen(false)} />}
    </div>
  )
}

// JoinCTA replaces the scope picker when the user belongs to no household.
// Clicking it opens the create-or-join modal. Once a membership exists,
// the regular two-pill switcher renders instead.
function JoinCTA({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="mx-3 mb-3 flex w-[calc(100%-1.5rem)] items-center gap-2 rounded-md border border-dashed border-indigo-300 bg-indigo-50/50 px-3 py-2 text-left text-xs text-indigo-700 hover:bg-indigo-50"
    >
      <Home size={14} className="shrink-0" />
      <span className="leading-tight">
        <span className="font-medium block">Create or join</span>
        <span className="text-[10px] text-indigo-500">a household</span>
      </span>
    </button>
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
            'min-h-[44px] flex-1 rounded px-2 py-1 text-xs font-medium transition md:min-h-0',
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
