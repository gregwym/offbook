// Default MSW handlers — cover every endpoint a page or hook hits on mount
// so smoke tests render the "happy path" without per-test boilerplate.
// Tests that need a different shape (loading/error/partial) override with
// `server.use(...)` for just that one route (see useScopedInsights.test.ts).
import { http, HttpResponse } from 'msw'
import * as fx from './fixtures'

const API = '/api/v1'
const list = <T,>(data: T[]) => HttpResponse.json({ data, total: data.length })
const item = <T,>(data: T) => HttpResponse.json({ data })

export const handlers = [
  // Auth / setup / scope
  http.get(`${API}/setup/status`, () => item({ bootstrapped: true, signup_mode: 'invite_only' })),
  http.get(`${API}/me`, () => item({ id: 1, user_id: 1 })),
  http.get(`${API}/me/scope`, () => item({ active: 'personal', available: ['personal'], household_id: null })),
  http.patch(`${API}/me/scope`, () => item({ active: 'personal', available: ['personal'], household_id: null })),
  http.get(`${API}/me/settings`, () => item(fx.userSettings)),
  http.patch(`${API}/me/settings`, () => item(fx.userSettings)),
  http.get(`${API}/health`, () => item({ status: 'ok', version: 'test-sha' })),

  // Categories / accounts / assets
  http.get(`${API}/categories`, () => list(fx.categories)),
  http.get(`${API}/accounts`, () => list(fx.accounts)),
  http.get(`${API}/assets`, () => list(fx.assets)),

  // Budgets / goals
  http.get(`${API}/budgets`, () => list(fx.budgets)),
  http.get(`${API}/budgets/:id/spend`, () => item(fx.budgetSpend1)),
  http.get(`${API}/savings-goals`, () => list(fx.goals)),

  // Transactions / rules
  http.get(`${API}/transactions`, () => list(fx.transactions)),
  http.get(`${API}/categorization-rules`, () => list(fx.rules)),

  // Dashboard (personal Insights band)
  http.get(`${API}/dashboard/summary`, () => item(fx.dashboardSummary)),
  http.get(`${API}/dashboard/net-worth`, () => list(fx.netWorthTrend)),
  http.get(`${API}/dashboard/allocation`, () => list(fx.allocation)),

  // Plaid (Settings — Linked Institutions)
  http.get(`${API}/plaid/items`, () => list(fx.plaidItems)),

  // Household aggregator + shared CRUD (household-scope smoke tests)
  http.get(`${API}/households/:id`, () => item(fx.householdDetail)),
  http.get(`${API}/households/:id/members`, () => item(fx.membersListing)),
  http.get(`${API}/households/:id/shared-budgets`, () => list([])),
  http.get(`${API}/households/:id/shared-goals`, () => list([])),
  http.get(`${API}/h/dashboard`, () => item(fx.householdDashboard)),
  http.get(`${API}/h/budgets/pace`, () => list([])),
  http.get(`${API}/h/goals/progress`, () => list([])),
  http.get(`${API}/h/insights/allocation`, () => list(fx.householdAllocation)),
  http.get(`${API}/h/insights/net-worth`, () => list(fx.householdNetWorthTrend)),
  http.get(`${API}/h/insights/accounts`, () => list(fx.householdAccountSummaries)),
]
