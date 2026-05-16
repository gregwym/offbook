import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { AccountsPage } from './pages/AccountsPage'
import { AIPage } from './pages/AIPage'
import { BudgetsPage } from './pages/BudgetsPage'
import { DashboardPage } from './pages/DashboardPage'
import { ImportPage } from './pages/ImportPage'
import { InvestmentsPage } from './pages/InvestmentsPage'
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
        <Route path="/transactions" element={<TransactionsPage />} />
        <Route path="/budgets" element={<BudgetsPage />} />
        <Route path="/savings-goals" element={<SavingsGoalsPage />} />
        <Route path="/investments" element={<InvestmentsPage />} />
        <Route path="/import" element={<ImportPage />} />
        <Route path="/ai" element={<AIPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
    </Routes>
  )
}
