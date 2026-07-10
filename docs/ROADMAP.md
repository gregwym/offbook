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

## M12 — Trustworthy Overview [DONE]

**Goal:** A user with real historical data (Plaid sandbox + statement imports) opens Insights or Accounts and sees a **correct, current, complete** picture of their financial state. The M10/ADR-0017 valuation engine is sound (#281, #282 closed); this milestone wires the overview surfaces to it and keeps prices fresh between imports. Owner direction (June 2026): get the financial-state overview right before any new feature surface.

- [x] #291 — Accounts + Insights read the dead `a.balance` field (dropped in M10) → expose the valuation-derived balance on the accounts API and consume it. (PR #342)
- [x] #341 — Personal allocation endpoint — the Insights allocation band was hardcoded empty in personal scope. (PR #343; found while scoping #291)
- [x] #339 — Render completeness/staleness signals on Insights + Accounts (the UI half of the #282 "no wrong-but-confident totals" contract). (PR #345)
- [x] #344 — Headline net-worth completeness flags, both scopes — the last silent missing-price-→-$0 coercions removed. (PR #348; found while scoping #339)
- [x] #338 — Epic: pluggable price & FX ingestion (ADR-0014, 3 phases): provider seam + CoinGecko + manual refresh (PR #346), Frankfurter/ECB FX (PR #347), opt-in daily scheduled refresh (final PR).

**Done criteria:** Accounts list and Insights bands show valuation-derived balances (no blank/0 from the dead field). A portfolio with manually tracked equity + crypto + foreign cash shows today's net worth without re-importing — manually via the Insights "Refresh prices" button, or daily via the opt-in Settings toggle. Any unpriced/stale figure is visibly flagged as partial rather than silently understated. Personal and household (`/h/insights`) both honor the above.

**Sequencing note:** M11 visual pass, AI advisor un-deferral, and forward-looking features (recurring detection, cash-flow forecast) intentionally wait behind this milestone.

---

## Production-Readiness Arc (M13–M17)

Owner direction (July 2026): make the project production ready for six product milestones — **A** spending analysis, **B** combined asset view, **C** manual import from non-Plaid institutions, **D** family of 2+ users, **E** single-user budgets & goals, **F** family budgets & goals. Full current-state review and rationale in [docs/PRODUCTION-READINESS.md](PRODUCTION-READINESS.md). Sequencing: **M13 strictly first**; M14 → M15 → M16 in order; M17 parallelizable after M13.

## M13 — Production Baseline [NEXT]

**Goal:** An instance you'd trust with real money and a family member's data. No new product surface. (Phase 0 of the production-readiness plan.) Tracked in epic #383.

- [ ] #357 — Backups & restore: nightly `pg_dump` (compose sidecar or systemd timer), retention, optional off-host copy; `make backup` / `make restore`; **scripted restore verification**; runbook.
- [ ] #358 — Migration safety: expand→migrate→contract policy in AGENTS.md; automatic pre-migration backup in `make deploy`; up→down→up round-trip test in `make verify`. The pre-prod "wipe and rebuild" era ends here.
- [ ] #351 — Fix P1: spending by_category leaks opening balances/trade legs (missing `kind='flow'` predicate) + regression tests.
- [ ] #352 — Fix P2 minimum slice: write trade prices as `prices` rows, `source='trade'` (full Tier-1 manual entry lands in M15 as #373).
- [ ] #359 — Job scheduling: generalize the in-app price scheduler (or systemd timers) to run `cmd/household-purge` daily and the ingestion-jobs purge (#337). Grace-period purge is a privacy promise — it must actually run.
- [ ] #360 — Monitoring & alerting: structured logs + notifier seam (ntfy/webhook/email) for failed jobs, Plaid item errors, failed deploys, low disk, backup failures.
- [ ] #272 / #275 — CI promotion: acceptance baseline smoke required on PR; frontend vitest + RTL + msw incl. the #266 regression test.
- [ ] #362 — Plaid **production** application submitted (long lead time — start day one). Sandbox stays the dev/QA default.
- [ ] #361 — Deploy/rollback runbook: pin-previous-image rollback, post-deploy SHA smoke.

**Done criteria:** Simulated volume loss recovered from last night's backup by runbook alone. A killed scheduler or errored Plaid item produces a notification. #351/#352 fixed with regression coverage. PRs gated by contract-check + unit + acceptance smoke. Plaid production application in flight.

## M14 — Spending Analysis GA (Milestone A)

**Goal:** Transactions flow in unattended, get categorized without user effort, and Insights answers "where does my money go." Tracked in epic #384.

- [ ] #363 — Scheduled background transaction sync per `plaid_item` (daily, jittered; **polling, not webhooks** — Tailscale-private hosts can't expose a public endpoint; record as ADR).
- [ ] #364 — Re-auth flow: `ITEM_LOGIN_REQUIRED`/`PENDING_EXPIRATION` → item `reauth_required` → Settings banner → Plaid Link update mode → resume.
- [ ] #365 — Sync-health UX + notifier hook on item error (DLQ badge already shipped).
- [ ] #195 — full Plaid PFC taxonomy sweep into `plaid_category_map`.
- [ ] #366 — **AI auto-categorization** (new ADR): `TransactionCategorizer` capability in `service/ai` mirroring the `DocumentExtractor` seam (Claude/OpenAI-compatible/Ollama). Precedence: manual > rule > AI > plaid_default. Batch pass over uncategorized only; PII ban enforced by `noimport`-style test; confidence-gated into the existing "Needs review" flow; merchant-verdict cache + "promote to rule"; per-instance daily AI budget.
- [ ] #367 / #368 — Analysis depth: month-over-month category trends, top merchants, income vs. spending trend; deterministic recurring/subscription detection (read-only band — never invents transactions).
- [ ] #190 — Mobile fixes (+ #192 if the advisor surface stays routed).

**Done criteria:** A production institution syncs daily unattended ≥2 weeks including one forced re-auth. ≥90% of new transactions land categorized (map+rules+AI) with AI never overriding manual/rule picks. Insights answers trend/merchant/recurring questions.

## M15 — Combined Asset View GA (Milestone B)

**Goal:** Net worth you can defend — every number traceable to positions × prices, every unexplained delta visible. Tracked in epic #385.

- [ ] #369 — Extend scheduled sync to balances + holdings; verify liability (credit card/loan) reconciliation signs; per-account "as of" provenance.
- [ ] #370 — **Reconciliation view** per account (balance-observation history, fold-vs-reported, every `opening_balance`/`adjustment` row linked to its causing observation) + unexplained-delta flag: adjustments above a threshold mark the account "needs attention" (adjustments are suspense entries to explain, not absorb).
- [ ] #371 — Transfer-matching pass proposing `is_transfer` pairs (opposite amounts, ±N days, cross-account).
- [ ] #372 — Equity/ETF price provider behind the ADR-0014 seam (keyless default, keyed opt-in) + scheduled refresh.
- [ ] #373 — Tier-1 manual price entry (finishes #352 properly): asset-level "set price" endpoint + UI, `source='manual'`.
- [ ] #374 — Historical net worth from observation + price history; date-range selector; asset-class drill-in.

**Done criteria:** Mixed cash/brokerage/crypto/multi-currency portfolio shows complete, unflagged net worth after scheduled refresh with zero manual re-import. A deliberately skipped statement surfaces as a flagged unexplained delta, never a silent number change.

## M16 — Statement Import GA (Milestone C)

**Goal:** A non-Plaid institution is first-class: one PDF a month keeps accounts, balances, and positions current. Tracked in epic #386.

- [ ] #375 — Positions + statement-balance extraction (extends ADR-0019): `ParsedPosition[]` alongside `ParsedRow[]`; deterministic re-validation; commit path reconciles via the existing `ReconcilePosition`; preview shows txns + positions + resulting adjustments.
- [ ] #336 — Ollama vision extractor (image-only first; PDF stays Claude-only, documented) for zero-egress import.
- [ ] #376 — Import ergonomics: per-account import history with doc-total reconciliation results; duplicate-statement detection via content hash.

**Done criteria:** A brokerage PDF from an unsupported institution yields transactions + positions + a reconciled balance through preview→commit; re-import is a no-op; an Ollama-only user imports statement photos with zero egress.

## M17 — Family & Plans GA (Milestones D + E + F)

**Goal:** The already-built household + budgets/goals surfaces get mechanical proof and the last UX gaps. Parallelizable after M13. Tracked in epic #387.

- [ ] #377 — Promote QA suites 6 (household privacy) + 7 (member lifecycle) to required CI.
- [ ] #378 — Invite UX: copyable invite link + surfaced expiry (no SMTP dependency).
- [ ] #379 — Two-user concurrent-sync race test (per-user Plaid item/ingestion isolation).
- [ ] #380 — Household Insights parity: aggregator privacy tests extended to allocation/net-worth bands (`balance_only` vs `balance_and_txns`).
- [ ] #381 — E: verify/fix budget `rollover` (or remove the column), near/over-budget notification via the M13 notifier, goal contribution history.
- [ ] #382 — F: per-member contribution attribution where visibility permits; shared over-budget alerts honoring `balance_only` exclusions; acceptance coverage.

**Done criteria:** QA.md suites 1–9 green in CI (cold-start stays opt-in). A two-user family runs both books + a shared budget/goal for a full statement cycle with correct aggregates and zero privacy-test regressions.

---

### M11+ — Frontend Hi-Fi: Visual Pass [DEFERRED]

Once M9 IA and M10 data model are in place, apply the visual hi-fi treatment from `docs/designs/Offbook Hi-Fi v1.html` — typography, color tokens, spacing, polished empty/loading/error states. Vertical-slice approach: pick one high-traffic surface (likely Insights or Transactions), establish the design-system reference there, then propagate.
