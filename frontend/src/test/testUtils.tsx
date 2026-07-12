// Shared render + store-reset helpers for page/hook tests. Zustand stores
// are module-level singletons, so without an explicit reset, state leaks
// between tests in the same file (and across files sharing a worker).
import type { ReactElement } from 'react'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { expect } from 'vitest'
import { useAccountsStore } from '../store/accountsStore'
import { useAssetsStore } from '../store/assetsStore'
import { useAuthStore } from '../store/authStore'
import { useCategoriesStore } from '../store/categoriesStore'
import { useHouseholdStore } from '../store/householdStore'
import { useRulesStore } from '../store/rulesStore'
import { useScopeStore } from '../store/scopeStore'
import { useTransactionsStore } from '../store/transactionsStore'
import { SCOPE_PERSONAL } from '../types/scope'

// renderPage mounts a page component the way AppShell would — inside a
// router (several pages use <Link>/useNavigate) — without pulling in the
// full app shell/sidebar.
export function renderPage(ui: ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

// resetStores restores every zustand store's data fields to its initial
// shape. Only data fields are touched (partial `set`, not replace) so the
// store's action functions stay intact.
export function resetStores() {
  useAuthStore.setState({ setup: null, user: { id: 1 }, hydrated: true, error: null })
  useScopeStore.setState({
    active: SCOPE_PERSONAL,
    available: [SCOPE_PERSONAL],
    householdId: null,
    hydrated: true,
    loading: false,
    error: null,
  })
  useAccountsStore.setState({ accounts: [], total: 0, loading: false, error: null, filter: {} })
  useAssetsStore.setState({ assets: [], loaded: false, loading: false, error: null })
  useCategoriesStore.setState({ categories: [], loaded: false, loading: false, error: null })
  useRulesStore.setState({ rules: [], loading: false, error: null, code: null })
  useTransactionsStore.setState({
    transactions: [],
    total: 0,
    loading: false,
    error: null,
    filter: {},
    page: 0,
    pageSize: 50,
  })
  useHouseholdStore.setState({ detail: null, loading: false, error: null, lastInvite: null })
}

// setHouseholdScope switches the shared scope store into household scope
// for the household-surface smoke tests.
export function setHouseholdScope(householdId: number) {
  useScopeStore.setState({
    active: 'household',
    available: ['personal', 'household'],
    householdId,
    hydrated: true,
    loading: false,
    error: null,
  })
}

// expectHealthySmoke is the "no error banner, no stuck loading state"
// assertion the L5 issue calls for, applied generically across pages: every
// page in this app renders its error banners with the same
// `border-red-200 bg-red-50` combination (see .claude/rules/frontend.md),
// and its loading placeholders as literal "Loading…" text.
export async function expectHealthySmoke() {
  await waitFor(() => {
    expect(screen.queryAllByText(/loading/i)).toHaveLength(0)
  })
  expect(document.querySelector('.border-red-200.bg-red-50')).toBeNull()
}
