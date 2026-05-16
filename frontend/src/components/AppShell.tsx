import {
  ArrowDownToLine,
  Bot,
  LayoutDashboard,
  PiggyBank,
  Receipt,
  Settings,
  Target,
  TrendingUp,
  Wallet,
} from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'

const NAV_ITEMS: Array<{ to: string; label: string; icon: typeof LayoutDashboard }> = [
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

export function AppShell() {
  return (
    <div className="flex h-screen w-screen bg-gray-50">
      <aside className="flex w-60 flex-col border-r border-gray-200 bg-white">
        <div className="px-6 py-5 text-lg font-semibold text-gray-900">offbook</div>
        <nav className="flex-1 space-y-1 px-3 pb-4">
          {NAV_ITEMS.map(({ to, label, icon: Icon }) => (
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
