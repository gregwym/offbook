import { useEffect } from 'react'
import { Navigate, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './store/authStore'
import { AccountsPage } from './pages/AccountsPage'
import { AIPage } from './pages/AIPage'
import { BudgetsPage } from './pages/BudgetsPage'
import { DashboardPage } from './pages/DashboardPage'
import { HouseholdAIPage } from './pages/HouseholdAIPage'
import { HouseholdBudgetsPage } from './pages/HouseholdBudgetsPage'
import { HouseholdDashboardPage } from './pages/HouseholdDashboardPage'
import { HouseholdGoalsPage } from './pages/HouseholdGoalsPage'
import { HouseholdMembersPage } from './pages/HouseholdMembersPage'
import { HouseholdSettingsPage } from './pages/HouseholdSettingsPage'
import { ImportPage } from './pages/ImportPage'
import { InvestmentsPage } from './pages/InvestmentsPage'
import { PlaidConnectPage } from './pages/PlaidConnectPage'
import { RulesPage } from './pages/RulesPage'
import { SavingsGoalsPage } from './pages/SavingsGoalsPage'
import { SettingsPage } from './pages/SettingsPage'
import { SetupAdminPage } from './pages/SetupAdminPage'
import { SigninPage } from './pages/SigninPage'
import { SignupPage } from './pages/SignupPage'
import { TransactionsPage } from './pages/TransactionsPage'

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
      {/* Unauthenticated routes — no AppShell, no auth required. */}
      <Route path="/setup/admin" element={<SetupAdminPage />} />
      <Route path="/signin" element={<SigninPage />} />
      <Route path="/signup" element={<SignupPage />} />

      {/* Authenticated routes — RequireAuth wraps the AppShell. */}
      <Route element={<RequireAuth />}>
        <Route element={<AppShell />}>
          <Route index element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/accounts" element={<AccountsPage />} />
          <Route path="/connect" element={<PlaidConnectPage />} />
          <Route path="/transactions" element={<TransactionsPage />} />
          <Route path="/rules" element={<RulesPage />} />
          <Route path="/budgets" element={<BudgetsPage />} />
          <Route path="/savings-goals" element={<SavingsGoalsPage />} />
          <Route path="/investments" element={<InvestmentsPage />} />
          <Route path="/import" element={<ImportPage />} />
          <Route path="/ai" element={<AIPage />} />
          <Route path="/settings" element={<SettingsPage />} />

          <Route path="/h/dashboard" element={<HouseholdDashboardPage />} />
          <Route path="/h/budgets"   element={<HouseholdBudgetsPage />} />
          <Route path="/h/goals"     element={<HouseholdGoalsPage />} />
          <Route path="/h/members"   element={<HouseholdMembersPage />} />
          <Route path="/h/ai"        element={<HouseholdAIPage />} />
          <Route path="/h/settings"  element={<HouseholdSettingsPage />} />
        </Route>
      </Route>
    </Routes>
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
