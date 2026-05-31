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
// who only ever visits /insights never downloads Recharts (Investments),
// the AI chat surface, or the Plaid SDK shim.
//
// React.lazy wants default exports; our pages are named exports, so we
// adapt each module inline. Keep the adaption shape identical so the
// Suspense + chunk splits stay symmetric across pages.
const InsightsPage = lazy(() => import('./pages/InsightsPage').then((m) => ({ default: m.InsightsPage })))
const AccountsPage = lazy(() => import('./pages/AccountsPage').then((m) => ({ default: m.AccountsPage })))
const AccountsAddPage = lazy(() => import('./pages/AccountsAddPage').then((m) => ({ default: m.AccountsAddPage })))
const TransactionsPage = lazy(() => import('./pages/TransactionsPage').then((m) => ({ default: m.TransactionsPage })))
const RulesPage = lazy(() => import('./pages/RulesPage').then((m) => ({ default: m.RulesPage })))
const BudgetsPage = lazy(() => import('./pages/BudgetsPage').then((m) => ({ default: m.BudgetsPage })))
const SavingsGoalsPage = lazy(() => import('./pages/SavingsGoalsPage').then((m) => ({ default: m.SavingsGoalsPage })))
const SettingsPage = lazy(() => import('./pages/SettingsPage').then((m) => ({ default: m.SettingsPage })))
const HouseholdMembersPage = lazy(() => import('./pages/HouseholdMembersPage').then((m) => ({ default: m.HouseholdMembersPage })))
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
          <Route index element={<Navigate to="/insights" replace />} />
          <Route path="/insights" element={<LazyRoute><InsightsPage /></LazyRoute>} />
          {/* Legacy dashboard path: keep redirecting so bookmarks survive. */}
          <Route path="/dashboard" element={<Navigate to="/insights" replace />} />
          <Route path="/accounts" element={<LazyRoute><AccountsPage /></LazyRoute>} />
          <Route path="/accounts/add" element={<LazyRoute><AccountsAddPage /></LazyRoute>} />
          {/* /connect and /import are absorbed by /accounts/add (v6 §03 + §07).
              Keep the old paths around as redirects so bookmarks still land
              somewhere useful — they'll be dropped entirely in a later cleanup. */}
          <Route path="/connect" element={<Navigate to="/accounts/add" replace />} />
          <Route path="/import" element={<Navigate to="/accounts/add" replace />} />
          <Route path="/transactions" element={<LazyRoute><TransactionsPage /></LazyRoute>} />
          <Route path="/rules" element={<LazyRoute><RulesPage /></LazyRoute>} />
          <Route path="/budgets" element={<LazyRoute><BudgetsPage /></LazyRoute>} />
          <Route path="/savings-goals" element={<LazyRoute><SavingsGoalsPage /></LazyRoute>} />
          {/* /investments was dissolved per ADR-0013 — allocation moved to
              Insights, holdings to Accounts, trades to Transactions. Keep
              the redirect so bookmarks survive (#268). */}
          <Route path="/investments" element={<Navigate to="/insights" replace />} />
          {/* /ai is deferred in v6 — AI provider config lives in Settings.
              Redirect bookmarks to Settings rather than 404. */}
          <Route path="/ai" element={<Navigate to="/settings" replace />} />
          <Route path="/settings" element={<LazyRoute><SettingsPage /></LazyRoute>} />

          {/* Household surfaces reuse the scope-agnostic pages — the active
              scope swaps the data source, not the component (v6 IA). */}
          <Route path="/h/insights"  element={<LazyRoute><InsightsPage /></LazyRoute>} />
          <Route path="/h/dashboard" element={<Navigate to="/h/insights" replace />} />
          <Route path="/h/budgets"   element={<LazyRoute><BudgetsPage /></LazyRoute>} />
          <Route path="/h/goals"     element={<LazyRoute><SavingsGoalsPage /></LazyRoute>} />
          <Route path="/h/members"   element={<LazyRoute><HouseholdMembersPage /></LazyRoute>} />
          {/* Household AI is deferred in v6, mirroring personal /ai. Redirect
              bookmarks to household Settings rather than 404. */}
          <Route path="/h/ai"        element={<Navigate to="/h/settings" replace />} />
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
