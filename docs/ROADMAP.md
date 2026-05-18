# Offbook Roadmap

## M0 — Repo & Autonomous Development Setup [DONE]

**Goal:** Self-sustaining development environment.

- [x] git init, create private GitHub repo
- [x] CLAUDE.md — scannable session guide
- [x] .claude/settings.json — permissions + hooks
- [x] .claude/hooks/ — branch name validation
- [x] .claude/rules/ — scoped rules (go-backend, database, frontend, testing)
- [x] docs/ARCHITECTURE.md — full technical reference
- [x] docs/ROADMAP.md — this file
- [x] docs/ADR/ — 5 initial ADRs
- [x] .github/ISSUE_TEMPLATE/feature.md
- [x] docker-compose.yml skeleton
- [x] .env.example
- [x] .gitignore
- [x] Initial commit and push to GitHub
- [x] Create M1 milestone and file all M1 issues (8 issues: #1–#8)

**Done criteria:** Repo on GitHub; CLAUDE.md + .claude/ infra complete; hooks enforce branch naming + go vet on commit; M1 backlog fully filed as GitHub Issues; a cold autonomous session can start M1 without asking any questions.

---

## M1 — Foundation [DONE]

**Goal:** Running Go + React skeleton end-to-end.

- [x] Go + Gin app skeleton with `/api/v1/health`
- [x] PostgreSQL connection + golang-migrate setup
- [x] Migration 000001: all tables created
- [x] Migration 000002: category seed data (20 system categories)
- [x] React + Vite + TypeScript scaffold
- [x] AppShell layout (sidebar nav, routing, all pages as stubs)
- [x] Docker Compose: backend + frontend + postgres services, volumes configured
- [x] shopspring/decimal integrated, verified in a unit test
- [x] golangci-lint configured

**Done criteria:** `docker compose up` → frontend at :5173, health 200, Postgres schema initialized; `go vet` and `golangci-lint` pass.

---

## M2 — Accounts & Transactions (Manual) [DONE]

**Goal:** Core data model usable through UI.

- [x] Accounts CRUD (handler + service + repo + frontend)
- [x] PII store: save/retrieve holder name and account number for accounts
- [x] Manual transaction entry (handler + service + repo + frontend)
- [x] Transaction list with filters (account, date range, category, search, pagination)
- [x] Category assignment (inline in transaction table)
- [x] Dashboard summary API + basic dashboard page
- [x] Go unit tests: account_service, transaction_service

**Done criteria:** Add account, enter transaction with PII, assign category, see dashboard totals; PII accessible only via `/accounts/:id/pii`.

---

## M2.5 — Households Foundation [DONE]

**Goal:** Structural foundation for multi-user / household scopes — data model, auth, scope switcher, aggregator. No hi-fi UI for household surfaces yet.

See [ADR-0006](ADR/0006-multi-tenant-model.md), [ADR-0007](ADR/0007-member-lifecycle.md), [ADR-0008](ADR/0008-household-aggregation-layer.md) and `docs/designs/App Hierarchy v4.html`.

- [x] Single foundation migration: `users`, `sessions`, `instance_config`, `households`, `household_members`, `household_invites`, `account_shares`, `shared_budgets`, `shared_goals`; rename `ai_conversations` → `ai_threads` with `user_id`/`household_id`/`shared_with_household`; add `user_id NOT NULL FK` to `accounts`/`transactions`/`budgets`/`savings_goals`/`investments`
- [x] Auth + sessions + first-`/signup`-becomes-admin + picks `signup_mode` (defaults `invite_only`); session middleware gates `/api/v1/*`
- [x] Household APIs: create, invite (token, gated by signup_mode), accept, leave (last-owner → 409), rejoin (auto-resume if `purged_at IS NULL`), owner sets `grace_period_days`
- [x] `account_shares` GET/PUT — 3-level visibility per account per household
- [x] `service/household/aggregator.go` — `Dashboard`, `BudgetPace`, `GoalProgress`, `AIContext`. NO `pii_repo` dependency.
- [x] Aggregator privacy tests: private excluded, `balance_only` excluded from category breakdown, in-grace excluded from live aggregates, no raw transactions in return types (reflection check), no cross-member chat leakage
- [x] `GET/PATCH /me/scope` — defaults to `household` when member, else `personal`
- [x] Frontend: zustand `scopeStore` + scope picker in sidebar + 6 household routes (`/h/dashboard`, `/h/budgets`, `/h/goals`, `/h/members`, `/h/ai`, `/h/settings`) as PageStubs

**Done criteria:** First boot → `/setup/admin` creates admin + picks `invite_only`. Invite a second user, both create households or join one. Account-level visibility toggles per account. Scope picker swaps the sidebar route list. Aggregator privacy tests green. Existing single-user flows still work for the bootstrap user. Members page UI, Household Dashboard layout, visibility-chip rendering, and Household AI Advisor wait for hi-fi.

**Out of scope (deferred):** Members table UI, Household Dashboard layout, visibility-chip visuals, Household AI Advisor UI, `shared_budgets`/`shared_goals` CRUD, `cmd/household-purge` runner.

---

## M3 — Plaid Sandbox Integration [DONE]

**Goal:** Real financial data flowing in via Plaid.

- [x] Plaid Link token endpoint + token exchange
- [x] Account discovery and creation from Plaid (PII → pii_store)
- [x] Transaction sync: initial full pull
- [x] Transaction sync: incremental (cursor-based)
- [x] Deduplication via plaid_transaction_id
- [x] Plaid category → internal category mapping
- [x] Sync status indicator per account
- [x] Frontend: PlaidConnect page with Plaid Link button

**Done criteria:** Connect Chase sandbox, transactions appear, re-sync = no duplicates; account holder name in pii_store.

---

## M4 — Categorization Engine [NOT STARTED]

**Goal:** Smart auto-categorization beyond Plaid defaults.

- [ ] Categorization rules CRUD (contains|regex|exact, priority-ordered)
- [ ] Rules applied on transaction import
- [ ] Bulk re-categorize endpoint
- [ ] Rules management UI
- [ ] "Create rule from this transaction" shortcut in transaction table

**Done criteria:** Create rule "WHOLEFDS → Groceries", re-apply to all transactions, verify mapping.

---

## M5 — Budgets & Savings Goals [NOT STARTED]

**Goal:** Planning and tracking features complete.

- [ ] Budget CRUD + current period spend calculation (NUMERIC arithmetic)
- [ ] Savings goals + contribution tracking
- [ ] Budget alerts in dashboard (>80% warning, >100% over-budget)
- [ ] Charts: spending by category (pie), cash flow by month (bar), net worth over time (line)

**Done criteria:** Set $700 grocery budget, import transactions, see >100% warning; net worth chart shows trend.

---

## M6 — Investments [NOT STARTED]

**Goal:** Portfolio tracking.

- [ ] Investment snapshot model (append-only)
- [ ] Manual holdings entry (NUMERIC quantity for crypto)
- [ ] CSV import for brokerage statements (Vanguard, Fidelity formats)
- [ ] Portfolio summary: total value, allocation by asset class
- [ ] Holdings table: cost basis, market value, unrealized G/L (all NUMERIC)
- [ ] Allocation donut chart

**Done criteria:** Enter 0.05123456789012345 BTC, see value without precision loss; enter VTSAX, see allocation chart.

---

## M7 — AI Advisor [NOT STARTED]

**Goal:** Privacy-preserving financial assistant using only DB data.

- [ ] AIProvider interface + ClaudeProvider (SSE streaming) + OllamaProvider
- [ ] context_builder.go — anonymized financial context from DB (no pii_repo in deps)
- [ ] AI service orchestration
- [ ] Chat UI: model switcher, context preview panel, conversation history
- [ ] Suggested prompts
- [ ] Settings page: Claude API key, Ollama URL

**Done criteria:** Chat with AI; context preview shows only aggregated data; pii_store data absent from context; switch to Ollama and chat works.

---

## Frontend Hi-Fi Milestones

Backend-first milestones (M2.5, M4–M7) intentionally ship with stub or
minimal UI to keep autonomous progress moving on data model and APIs. Each
backend milestone spawns a corresponding frontend milestone that turns the
stubs into real user flows. The yardstick is each issue's **Product Goal**
(see `.github/ISSUE_TEMPLATE/feature.md`) — done = a user can complete the
goal through the UI, not just that the endpoint exists.

### M8 — Frontend Hi-Fi: Auth & Households [NOT STARTED]

**Goal:** Replace M2.5 PageStubs with real UI for the auth + household surfaces.

- [ ] First-boot `/setup/admin` page (admin creation + signup_mode picker)
- [ ] `/signin` page + session cookie handling + redirect logic
- [ ] `/signup` page (gated by `signup_mode`; invite-token form in invite_only mode)
- [ ] `/h/members` — list members, roles, in-grace badges, owner actions (invite, remove, set `grace_period_days`)
- [ ] `/h/dashboard` — household-aggregate dashboard layout (consumes `aggregator.Dashboard`)
- [ ] Account visibility chips on `/accounts` (per-household: private / balance-only / balance-and-txns)
- [ ] `/h/settings` — household name, owner transfer, grace period, leave button
- [ ] Scope-switcher polish: empty-state when not in a household, "create or join" CTA

**Done criteria:** Fresh `docker compose up` → admin signup → invite a second user → second user joins → both see members page + household dashboard with real aggregate data → second user leaves → admin sees them in-grace → admin sets grace to 0 → purge runs. Every step clickable in the UI.

### M9+ — Frontend Hi-Fi: Per-Feature [DEFERRED]

Each future backend milestone (M4 categorization, M5 budgets, M6 investments, M7 AI) gets a paired frontend milestone filed when the backend milestone closes. Pattern:
- File each FE issue with a one-sentence **Product Goal** (see issue template)
- Done = a user can reach the product goal through the UI
