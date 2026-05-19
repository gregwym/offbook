import { lazy, Suspense, useEffect } from 'react'
import { Navigate, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './store/authStore'
// Auth-flow pages are eager — they're the front door before sign-in and
// shouldn't incur a Suspense flash on the very first paint.
import { SetupAdminPage } from './pages/SetupAdminPage'
import { SigninPage } from './pages/SigninPage'
import { SignupPage } from './pages/SignupPage'

// Authenticated routes lazy-load their page modules. This drops the
// initial bundle below Vite's 500 kB warning threshold and means a user
// who only ever visits /dashboard never downloads Recharts (Investments),
// the AI chat surface, or the Plaid SDK shim.
//
// React.lazy wants default exports; our pages are named exports, so we
// adapt each module inline. Keep the adaption shape identical so the
// Suspense + chunk splits stay symmetric across pages.
const DashboardPage = lazy(() => import('./pages/DashboardPage').then((m) => ({ default: m.DashboardPage })))
const AccountsPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountsPage })))
const PlaidConnectPage = lazy(() => import('./pages/PlaidConnectPage').then((m) => ({ default: m.PlaidConnectPage })))
const TransactionsPage = lazy(() => import('./pages/TransactionsPage').then((m) => ({ default: m.TransactionsPage })))
const RulesPage = lazy(() => import('./pages/RulesPage').then((m) => ({ default: m.RulesPage })))
const BudgetsPage = lazy(() => import('./pages/BudgetsPage').then((m) => ({ default: m.BudgetsPage })))
const SavingsGoalsPage = lazy(() => import('./pages/SavingsGoalsPage').then((m) => ({ default: m.SavingsGoalsPage })))
const InvestmentsPage = lazy(() => import('./pages/InvestmentsPage').then((m) => ({ default: m.InvestmentsPage })))
const ImportPage = lazy(() => import('./pages/ImportPage').then((m) => ({ default: m.ImportPage })))
const AIPage = lazy(() => import('./pages/AIPage').then((m) => ({ default: m.AIPage })))
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))
const HouseholdDashboardPage = lazy(() => import('./pages/HouseholdDashboardPage').then((m) => ({ default: m.HouseholdDashboardPage })))
const HouseholdBudgetsPage = lazy(() => import('./pages/HouseholdBudgetsPage').then((m) => ({ default: m.HouseholdBudgetsPage })))
const HouseholdGoalsPage = lazy(() => import('./pages/HouseholdGoalsPage').then((m) => ({ default: m.HouseholdGoalsPage })))
const HouseholdMembersPage = lazy(() => import('./pages/HouseholdMembersPage').then((m) => ({ default: m.HouseholdMembersPage })))
const HouseholdAIPage = lazy(() => import('./pages/HouseholdAIPage').then((m) => ({ default: m.HouseholdAIPage })))
const HouseholdSettingsPage = lazy(() => import('./pages/HouseholdSettingsPage').then((m) => ({ default: m.HouseholdSettingsPage })))

export default function App() {
  const { hydrated, hydrate, signout } = useAuthStore()
  const navigate = useNavigate()

  // Register the global 401 handler once. When any authenticated API call
  // returns 401, drop the in-memory user and bounce to /signin — covers
  // the "session expired in another tab" case without a page reload.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      void signout()
      navigate('/signin', { replace: true })
    })
  }, [navigate, signout])

  useEffect(() => {
    if (!hydrated) void hydrate()
  }, [hydrated, hydrate])

  return (
    <Routes>
      {/* Unauthenticated routes — no AppShell, no auth required, eager-loaded. */}
      <Route path="/setup/admin" element={<SetupAdminPage />} />
      <Route path="/signin" element={<SigninPage />} />
      <Route path="/signup" element={<SignupPage />} />

      {/* Authenticated routes — RequireAuth wraps the AppShell. */}
      <Route element={<RequireAuth />}>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<LazyRoute><DashboardPage /></LazyRoute>} />
          <Route path="/accounts" element={<LazyRoute><AccountsPage /></LazyRoute>} />
          <Route path="/connect" element={<LazyRoute><PlaidConnectPage /></LazyRoute>} />
          <Route path="/transactions" element={<LazyRoute><TransactionsPage /></LazyRoute>} />
          <Route path="/rules" element={<LazyRoute><RulesPage /></LazyRoute>} />
          <Route path="/budgets" element={<LazyRoute><BudgetsPage /></LazyRoute>} />
          <Route path="/savings-goals" element={<LazyRoute><SavingsGoalsPage /></LazyRoute>} />
          <Route path="/investments" element={<LazyRoute><InvestmentsPage /></LazyRoute>} />
          <Route path="/import" element={<LazyRoute><ImportPage /></LazyRoute>} />
          <Route path="/ai" element={<LazyRoute><AIPage /></LazyRoute>} />
          <Route path="/settings" element={<LazyRoute><SettingsPage /></LazyRoute>} />

          <Route path="/h/dashboard" element={<LazyRoute><HouseholdDashboardPage /></LazyRoute>} />
          <Route path="/h/budgets"   element={<LazyRoute><HouseholdBudgetsPage /></LazyRoute>} />
          <Route path="/h/goals"     element={<LazyRoute><HouseholdGoalsPage /></LazyRoute>} />
          <Route path="/h/members"   element={<LazyRoute><HouseholdMembersPage /></LazyRoute>} />
          <Route path="/h/ai"        element={<LazyRoute><HouseholdAIPage /></LazyRoute>} />
          <Route path="/h/settings"  element={<LazyRoute><HouseholdSettingsPage /></LazyRoute>} />
        </Route>
      </Route>
    </Routes>
  )
}

// LazyRoute wraps every code-split route with the same Suspense fallback
// so the placeholder shape is uniform. Centered so the AppShell sidebar
// stays visible while the chunk loads.
function LazyRoute({ children }: { children: React.ReactNode }) {
  return (
    <Suspense
      fallback={
        <div className="flex h-full min-h-[60vh] items-center justify-center text-sm text-gray-400">
          Loading…
        </div>
      }
    >
      {children}
    </Suspense>
  )
}

// RequireAuth gates everything under the AppShell. Three states:
//   - not hydrated → render nothing (avoids an auth flicker)
//   - bootstrapped=false → redirect to /setup/admin
//   - no user → redirect to /signin
//   - user present → render the protected tree via <Outlet/> (AppShell wraps it)
function RequireAuth() {
  const { hydrated, setup, user } = useAuthStore()

  if (!hydrated) {
    return (
      <div className="flex h-screen w-screen items-center justify-center bg-gray-50 text-sm text-gray-400">
        Loading…
      </div>
    )
  }
  if (setup && !setup.bootstrapped) return <Navigate to="/setup/admin" replace />
  if (!user) return <Navigate to="/signin" replace />
  // <Outlet/> renders the nested AppShell + child route.
  return <Outlet />
}
