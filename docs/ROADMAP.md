# Offbook Roadmap

## M0 — Repo & Autonomous Development Setup [IN PROGRESS]

**Goal:** Self-sustaining development environment.

- [x] git init, create private GitHub repo
- [x] CLAUDE.md — scannable session guide
- [x] .claude/settings.json — permissions + hooks
- [x] .claude/hooks/ — branch name validation
- [x] .claude/rules/ — scoped rules (go-backend, database, frontend, testing)
- [x] docs/ARCHITECTURE.md — full technical reference
- [x] docs/ROADMAP.md — this file
- [ ] docs/ADR/ — 5 initial ADRs
- [ ] .github/ISSUE_TEMPLATE/feature.md
- [ ] docker-compose.yml skeleton
- [ ] .env.example
- [ ] .gitignore
- [ ] Initial commit and push to GitHub
- [ ] Create M1 milestone and file all M1 issues

**Done criteria:** Repo on GitHub; CLAUDE.md + .claude/ infra complete; hooks enforce branch naming + go vet on commit; M1 backlog fully filed as GitHub Issues; a cold autonomous session can start M1 without asking any questions.

---

## M1 — Foundation [NOT STARTED]

**Goal:** Running Go + React skeleton end-to-end.

- [ ] Go + Gin app skeleton with `/api/v1/health`
- [ ] PostgreSQL connection + golang-migrate setup
- [ ] Migration 000001: all tables created
- [ ] Migration 000002: category seed data (20 system categories)
- [ ] React + Vite + TypeScript scaffold
- [ ] AppShell layout (sidebar nav, routing, all pages as stubs)
- [ ] Docker Compose: backend + frontend + postgres services, volumes configured
- [ ] shopspring/decimal integrated, verified in a unit test
- [ ] golangci-lint configured

**Done criteria:** `docker compose up` → frontend at :5173, health 200, Postgres schema initialized; `go vet` and `golangci-lint` pass.

---

## M2 — Accounts & Transactions (Manual) [NOT STARTED]

**Goal:** Core data model usable through UI.

- [ ] Accounts CRUD (handler + service + repo + frontend)
- [ ] PII store: save/retrieve holder name and account number for accounts
- [ ] Manual transaction entry (handler + service + repo + frontend)
- [ ] Transaction list with filters (account, date range, category, search, pagination)
- [ ] Category assignment (inline in transaction table)
- [ ] Dashboard summary API + basic dashboard page
- [ ] Go unit tests: account_service, transaction_service

**Done criteria:** Add account, enter transaction with PII, assign category, see dashboard totals; PII accessible only via `/accounts/:id/pii`.

---

## M3 — Plaid Sandbox Integration [NOT STARTED]

**Goal:** Real financial data flowing in via Plaid.

- [ ] Plaid Link token endpoint + token exchange
- [ ] Account discovery and creation from Plaid (PII → pii_store)
- [ ] Transaction sync: initial full pull
- [ ] Transaction sync: incremental (cursor-based)
- [ ] Deduplication via plaid_transaction_id
- [ ] Plaid category → internal category mapping
- [ ] Sync status indicator per account
- [ ] Frontend: PlaidConnect page with Plaid Link button

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
