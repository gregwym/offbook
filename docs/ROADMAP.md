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

**Done criteria:** First boot → `/setup/admin` creates admin + picks `invite_only`. Invite a second user, both create households or join one. Account-level visibility toggles per account. Scope picker swaps the sidebar route list. Aggregator privacy tests green. Existing single-user flows still work for the bootstrap user.

**Originally deferred — all shipped post-M8:** Members table UI (#140), Household Dashboard layout (#141), visibility-chip rendering (#142), Household AI Advisor UI (#167), `shared_budgets` CRUD (#163), `shared_goals` CRUD (#165), `cmd/household-purge` runner (#161).

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

## M4 — Categorization Engine [DONE]

**Goal:** Smart auto-categorization beyond Plaid defaults.

- [x] Categorization rules CRUD (contains|regex|exact, priority-ordered)
- [x] Rules applied on transaction import
- [x] Bulk re-categorize endpoint
- [x] Rules management UI
- [x] "Create rule from this transaction" shortcut in transaction table

**Done criteria:** Create rule "WHOLEFDS → Groceries", re-apply to all transactions, verify mapping.

---

## M5 — Budgets & Savings Goals [DONE]

**Goal:** Planning and tracking features complete.

- [x] Budget CRUD + current period spend calculation (NUMERIC arithmetic)
- [x] Savings goals + contribution tracking
- [x] Budget alerts in dashboard (>80% warning, >100% over-budget)
- [x] Charts: spending by category (pie), cash flow by month (bar), net worth over time (line)

**Done criteria:** Set $700 grocery budget, import transactions, see >100% warning; net worth chart shows trend.

---

## M6 — Investments [DONE]

**Goal:** Portfolio tracking.

- [x] Investment snapshot model (append-only)
- [x] Manual holdings entry (NUMERIC quantity for crypto)
- [x] CSV import for brokerage statements (Vanguard, Fidelity formats)
- [x] Portfolio summary: total value, allocation by asset class
- [x] Holdings table: cost basis, market value, unrealized G/L (all NUMERIC)
- [x] Allocation donut chart

**Done criteria:** Enter 0.05123456789012345 BTC, see value without precision loss; enter VTSAX, see allocation chart.

**Backlog landed:** today's P&L tile (#122) — shipped as the snapshot-pair "Recent change" tile (between the two most recent snapshot dates per holding, no external price feed needed).

---

## M7 — AI Advisor [DONE]

**Goal:** Privacy-preserving financial assistant using only DB data.

- [x] AIProvider interface + ClaudeProvider (SSE streaming) + OllamaProvider
- [x] context_builder.go — anonymized financial context from DB (no pii_repo in deps)
- [x] AI service orchestration
- [x] Chat UI: model switcher, context preview panel, conversation history
- [x] Suggested prompts
- [x] Settings page: Claude API key, Ollama URL

**Done criteria:** Chat with AI; context preview shows only aggregated data; pii_store data absent from context; switch to Ollama and chat works.

---

## Frontend Hi-Fi Milestones

Backend-first milestones (M2.5, M4–M7) intentionally ship with stub or
minimal UI to keep autonomous progress moving on data model and APIs. Each
backend milestone spawns a corresponding frontend milestone that turns the
stubs into real user flows. The yardstick is each issue's **Product Goal**
(see `.github/ISSUE_TEMPLATE/feature.md`) — done = a user can complete the
goal through the UI, not just that the endpoint exists.

### M8 — Frontend Hi-Fi: Auth & Households [DONE]

**Goal:** Replace M2.5 PageStubs with real UI for the auth + household surfaces.

- [x] First-boot `/setup/admin` page (admin creation + signup_mode picker)
- [x] `/signin` page + session cookie handling + redirect logic
- [x] `/signup` page (gated by `signup_mode`; invite-token form in invite_only landed in #145)
- [x] `/h/members` — list members, roles, owner-mint invite; in-grace badges + owner-side moderation landed in #147
- [x] `/h/dashboard` — household-aggregate dashboard layout (consumes `aggregator.Dashboard`); per-member tiles landed in #149
- [x] Account visibility chips on `/accounts` (per-household: private / balance-only / balance-and-txns)
- [x] `/h/settings` — household name, grace period, leave button; owner transfer landed in #152
- [x] Scope-switcher polish: empty-state when not in a household, "create or join" CTA

**Done criteria:** Fresh `docker compose up` → admin signup → invite a second user → second user joins → both see members page + household dashboard with real aggregate data → second user leaves → admin sees them in-grace → admin sets grace to 0 → purge runs. Every step clickable in the UI.

**Backlog landed post-M8:** signup-with-invite endpoint (#145), owner-side member moderation (#147), per-member dashboard tiles (#149), owner-transfer endpoint (#152).

### M9 — Frontend Hi-Fi: v6 Personal Scope [DONE]

**Goal:** Restructure personal-scope IA to match `docs/designs/App Hierarchy v6.html`. Five durable surfaces, no wrapper: Sign up · Add account · Transactions · Insights · Settings. Household pages collapse onto the same components — scope swaps the data source, not the page.

Locked v6 decisions (see design doc for full rationale):
1. **Add account** — two tiles up front (Connect bank / Add manually). "Connect bank" hands off to the Plaid *native* picker; dismissing it falls back to manual within the same surface.
2. **Auto-categorize** — silent on import. Surfaced only via a banner + "Needs review" filter on the Transactions page.
3. **Manual import** — folded into Add Account and per-account "add more" affordances. No top-level `/import` route.
4. **Review** — one Insights page with 5 bands: net worth · allocation · spending · budgets · goals. Replaces Dashboard for both scopes.
5. **AI chat** — deferred. Provider config lives once in Settings; no sidebar route in personal scope.

Issues:
- [x] #224 — Scope-agnostic page foundation; consolidate Budgets + Goals pairs
- [x] #225 — Insights page (5 bands, replaces Dashboard for both scopes)
- [x] #226 — Sidebar + route restructure (drop `/import`, drop personal `/ai`, AI provider → Settings)
- [x] #227 — Add Account two-tile picker (absorbs `/import` and `/connect`)
- [x] #228 — Transactions: silent auto-categorize + "Needs review" banner & filter
- [x] #229 — Sign up strip-down (3 fields; kill vault/recovery/AI language)

**Done criteria:** Fresh `docker compose up` → sign up (3 fields) → land on empty `/insights` → click "Add your first account" → two-tile picker → Plaid sandbox flow OR manual fallback → transactions appear with silent auto-categorize → "Needs review" banner surfaces low-confidence rows → Insights page shows all 5 bands populated. Household scope routes (`/h/insights`, `/h/budgets`, `/h/goals`) render via the same components as personal, with aggregator-sourced data. No `Household*Page.tsx` duplicates remain for surfaces shared between scopes.

**Resolved open question:** household AI surface (`/h/ai`) **follows personal in being deferred** — the sidebar entry and route are removed (route redirects to `/h/settings`, mirroring personal `/ai` → `/settings`), and `HouseholdAIPage.tsx` is dropped. Provider config lives once in Settings. AI infra (`AIChatSurface`, `aiStore`, `api/ai`) is preserved unrouted for when AI is un-deferred.

**Consolidation note (#224):** the Budgets + Goals scope-agnostic consolidation was finished after the issue was first closed — `BudgetsPage`/`SavingsGoalsPage` now serve both personal and `/h/*` routes via `useScopedBudgets()`/`useScopedGoals()`, and the `HouseholdBudgetsPage`/`HouseholdGoalsPage` duplicates are deleted. No `Household*Page.tsx` remains for a surface shared between scopes.

---

## M10 — Unified Position Model [DONE]

**Goal:** Refactor account valuation to a position-based model. Every account is a bag of positions; **quantities are facts**, **valuations derive from positions × prices** (never stored). Closes the multi-currency, brokerage-cash-sleeve, trade-visibility, and household-allocation gaps the current two-shape schema (`accounts.balance` scalar + separate `investments` snapshots) leaves open.

See [ADR-0013](ADR/0013-position-based-account-model.md) for the full rationale. Pre-prod — we wipe dev DBs and rebuild rather than migrating data. Two integrated PRs land the refactor:

- [x] #231 — Foundation tables (`assets`, `positions`, `prices`) + read-only repositories. Landed via PR #236. Triggers + backfill in this PR are scaffolding the next step deletes.
- [x] #237 — M10a: drop legacy columns + drop `investments` table + add `transactions.asset_id` + rewrite all service reads + Plaid sync writes positions/prices + household aggregator gains `Allocation`/`NetWorthTrend`/`AccountSummaries` (closes M9 #225 gap).
- [x] #238 — M10b: trade ingestion — Plaid investment-transactions → paired-row trades; manual trade form; cost-basis recompute (average cost).

**Invariant carried throughout:** the app never invents transactions. Trade rows come from real import sources (Plaid, statement CSV) or explicit user input — never synthesized to reconcile a holdings-snapshot delta.

**Done criteria:** A user with mixed cash + brokerage + crypto accounts (across multiple currencies) sees net worth, allocation, and per-asset values computed from `positions × prices` with FX conversion to their primary currency. Plaid trades appear as paired rows in the Transactions list. Household scope shows asset allocation with per-account share visibility honored. `accounts.balance`, `investments.market_value`, `transactions.currency`, and the `investments` table are gone from the schema.

**Deferred to follow-up ADRs:**
- ADR-0014: Pluggable Tier-3 price provider (Yahoo, Polygon, ECB, CoinGecko).
- ADR-0015: Tax-lot precision opt-in (FIFO/LIFO/spec ID).

---

### M11+ — Frontend Hi-Fi: Visual Pass [DEFERRED]

Once M9 IA and M10 data model are in place, apply the visual hi-fi treatment from `docs/designs/Offbook Hi-Fi v1.html` — typography, color tokens, spacing, polished empty/loading/error states. Vertical-slice approach: pick one high-traffic surface (likely Insights or Transactions), establish the design-system reference there, then propagate.
