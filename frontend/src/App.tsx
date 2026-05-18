import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
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
import { TransactionsPage } from './pages/TransactionsPage'

export default function App() {
  return (
    <Routes>
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
    </Routes>
  )
}
