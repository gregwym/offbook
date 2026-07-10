# Production Readiness Review & Plan

**Date:** 2026-07-10 · **Reviewed at:** `549ad3f` (main)
**Scope:** take Offbook from "feature-complete dev instance" to production-ready for six owner milestones:

| Owner milestone | Summary | Current completion |
|---|---|---|
| **A** | Spending analysis (Plaid ingest, AI categorization, spending stats) | ~65% |
| **B** | Combined asset view (balances/positions ingest, reconciliation, net-worth views) | ~75% |
| **C** | Manual import from non-Plaid institutions (AI PDF → txns/balances/positions) | ~55% |
| **D** | Family of 2+ users (invites, per-user ingestion, shared aggregates) | ~85% |
| **E** | Single-user budgets & goals | ~90% |
| **F** | Family-level budgets & goals | ~85% |

The percentages are functional coverage against the milestone as worded, not effort remaining — the remaining slices are disproportionately the hard "reliably / production" parts.

---

## Part 1 — Current-State Review

### 1.1 Product

**What a user can do today (all shipped, M0–M12):**

- Sign up (invite-only or open), create/join exactly one household, per-account 3-level sharing (`private` / `balance_only` / `balance_and_txns`).
- Link Plaid **sandbox** institutions; cursor-based incremental transaction sync with dedup, per-row error DLQ + replay (ADR-0011), per-institution resync, sync-status surfacing.
- Plaid investment holdings + investment transactions → paired trade rows (M10b); positions × prices valuation model (ADR-0013/0017), FX conversion, completeness/staleness flags end-to-end (M12).
- **Reconciliation engine already exists** (ADR-0017, `service/reconcile.go`): every reported balance becomes a `balance_observation`; the transaction-ledger fold is compared to it and a `opening_balance` / `adjustment` transaction is written for the delta, idempotently, in the same DB transaction. Wired into Plaid account and holdings sync.
- Import transactions via CSV (auto-detected layout) and via **AI statement import** (PDF + photo, Claude vision, ADR-0019) with deterministic re-validation, confidence gating, preview → commit, per-import consent + audit.
- Categorization: user rules (contains/regex/exact, priority) + static Plaid PFC taxonomy map. Silent on import, "Needs review" banner/filter.
- Insights (personal + household, same components): net worth · allocation · spending · budgets · goals, valuation-derived, partial-data flags.
- Budgets & savings goals, personal and household, unified single-table model (ADR-0018); aggregator-mediated household pace/progress.
- Prices: CoinGecko (crypto) + Frankfurter (FX) providers, manual "Refresh prices", opt-in daily in-app scheduler (ADR-0014 phases 1–3).
- AI advisor chat (Claude / Ollama / OpenAI-compatible), PII-isolated by construction, context preview.

**Product gaps against the milestones:**

| # | Gap | Hits |
|---|---|---|
| P1 | **No AI categorization.** Categorization is rules + a static SQL-seeded Plaid map. Milestone A2 is not started. | A |
| P2 | **Spending analysis is shallow.** One by-category band for the current period + dashboard charts. No month-over-month, merchant view, or recurring detection. And it's currently *wrong*: bug **#351** (P1) leaks opening balances/trade legs into by-category. | A |
| P3 | **Sync is pull-only and user-triggered.** No background transaction/balance sync; data goes stale unless the user clicks resync. No `ITEM_LOGIN_REQUIRED` → Link update-mode re-auth flow. Sandbox only — no Plaid production credentials, OAuth-institution flows untested. | A, B |
| P4 | **Manually tracked equities value at $0 forever** (**#352**): no equity price provider (ADR-0014 deferred Yahoo/Polygon) and Tier-1 manual price entry (promised in ADR-0013) never shipped. | B |
| P5 | **Reconciliation is not surfaced.** Adjustments land as ledger rows but there is no per-account reconciliation view (observation history, unexplained-delta flag, "why did my balance jump"). Milestone B2's "surfaced using accounting best practices" is the missing half. | B |
| P6 | **AI statement import extracts transactions only.** `DocTotals` capture closing balance for reconciliation, but there is no positions/balances extraction path for brokerage statements (Milestone C1 wants all three). Claude-only (**#336** Ollama vision open); abandoned staging jobs never purged (**#337**). | C |
| P7 | Mobile defects on secondary surfaces: **#190** (Rules table overflow), **#192** (AI advisor clipped). | polish |
| P8 | Household (D) and plans (E/F) are functionally built; what's missing is *proof* — the household privacy/lifecycle acceptance suites (QA.md suites 6–7) are not promoted, and invite delivery is copy-a-token only. | D, E, F |

### 1.2 Engineering

**Strengths (keep doing):** clean handler→service→repo layering; PII isolation enforced structurally (+ `noimport` tests); `NUMERIC(30,18)`/`shopspring/decimal` everywhere; event-sourced quantities (ADR-0017) — the single hardest thing to retrofit is already the foundation; migration discipline + generated schema report + CI drift check; `contract-check` (frontend URL ↔ backend route) closing the #266/#268 bug class; race-enabled test suite; 19 ADRs recording every fork.

**Gaps:**

| # | Gap | Notes |
|---|---|---|
| E1 | **No frontend unit tests** (#275). `useScopedInsights`-class state-machine bugs have no fast net. | Epic #270 L5 |
| E2 | **Acceptance smoke not CI-gated** (#272). Baseline route smoke exists but is opt-in, so route-level breakage can still merge. | Epic #270 L2 |
| E3 | No background job framework beyond the in-app price scheduler; each new periodic need (sync, purges) is currently ad hoc. | Small: extend the existing `prices.Scheduler` pattern |
| E4 | No rate limiting / lockout on auth endpoints; fine on a Tailscale-only network, a real gap if any instance is ever exposed. | Document the assumption; cheap middleware |
| E5 | AI spend is uncapped: statement import + advisor + (future) categorization all call paid APIs with no per-user/instance budget or token accounting. | Needed before A2 scales the call volume |

### 1.3 Operations

**Strengths:** near-zero-config `make deploy` (FLAVOR convention, secrets-only env file, generated `SESSION_SECRET`); per-instance Tailscale sidecar = private-by-default HTTPS + MagicDNS (ADR-0016); pull-based auto-deploy systemd timer; strict env/DB isolation (`APP_ENV`, #183); build SHA in `/health` and Settings; Plaid access-token encryption at rest (ADR-0010).

**Gaps — these, not features, are the real production blockers:**

| # | Gap | Severity |
|---|---|---|
| O1 | **No backup or restore story at all.** No `pg_dump`, no volume snapshot, no restore runbook, nothing off-host. For a finance app this is the #1 blocker: one bad volume = total data loss. | Blocker |
| O2 | **"Pre-prod: wipe and rebuild" mentality must end.** M10 shipped by dropping dev DBs. The moment real money data exists, every migration must be forward-safe, down-tested, and preceded by an automatic backup. | Blocker |
| O3 | **No monitoring/alerting.** `/health` exists but nothing watches it; a failed nightly sync, a stuck scheduler, or a full disk is silent until the user notices stale numbers. | High |
| O4 | Maintenance runners aren't scheduled: `cmd/household-purge` (grace expiry) and the future ingestion-jobs purge (#337) exist/planned as CLIs but nothing invokes them. Grace-period purge is a *privacy promise* — an unscheduled purge runner is a quiet contract violation. | High |
| O5 | Plaid production access is a **process** dependency (application, review, OAuth registration, per-institution enablement) with weeks of lead time. Also: webhooks can't reach a Tailscale-private host — which is fine, but makes scheduled polling (P3) mandatory rather than optional. | High (lead time) |
| O6 | No update/rollback runbook: auto-deploy tracks origin/main with no staged rollout, no "previous image" rollback, no post-deploy smoke. | Medium |

---

## Part 2 — The Plan

Sequenced as five phases. Phase 0 is cross-cutting and comes first because every later milestone inherits its guarantees. Phases 1–3 map to owner milestones A, B, C. Phase 4 closes D/E/F, which are mostly verification and polish. Suggested roadmap labels M13–M17 (roadmap sections added alongside this doc).

### Phase 0 — M13 · Production Baseline (cross-cutting, do first)

*Goal: an instance you'd trust with real money and a spouse's data. No new product surface.*

1. **Backups & restore (O1).**
   - Nightly `pg_dump` via a compose sidecar or systemd timer (mirror `infra/auto-deploy` template pattern); retain N dailies + M weeklies; optional off-host copy (restic → any rclone target, user-configurable).
   - `make backup` / `make restore BACKUP=<file>` one-liners; restore runbook in `docs/dev/`.
   - **Scripted restore test** — a backup that's never been restored is not a backup. Verify restore into a scratch DB in CI or a scheduled job.
2. **Migration safety (O2).** Policy in AGENTS.md: production migrations are expand→migrate→contract; `make deploy` takes an automatic pre-migration backup; a migration-round-trip test (up → down → up) joins `make verify`.
3. **Correctness bugs that poison trust:** fix **#351** (P1 — spending band shows opening balances; one missing `AND kind='flow'` predicate + regression test) and **#352** (P2 — write trade prices as `prices` rows `source='trade'` as the minimum fix; full Tier-1 manual entry moves to Phase 2).
4. **Scheduling & lifecycle (O4):** generalize the in-app `prices.Scheduler` into a small job runner (or add systemd timers) covering household-purge (daily) and ingestion-jobs purge (#337, 7-day retention). Log every run.
5. **Monitoring & alerting (O3):** structured JSON logs; a lightweight notifier seam (ntfy/webhook/email — self-host friendly) alerting on: failed scheduled job, Plaid item entering `error`, failed deploy, low disk, backup failure. Uptime = any external pinger against `/health` over Tailscale.
6. **CI promotion (E1, E2):** land #272 (baseline acceptance smoke required on PR) and #275 (vitest + RTL + msw with the #266 regression test). Keeps everything after this phase honest.
7. **Plaid production application (O5) — start now**, it's the long pole: production keys, OAuth institution registration, security questionnaire. Sandbox remains the dev/QA default; production creds only in `.env.prod`.
8. **Ops runbook (O6):** deploy/rollback procedure (pin previous image tag), post-deploy smoke (`/health` SHA check — already have the data), and a short "disaster day" doc.

**Done when:** a simulated volume loss is recovered from last night's backup by following the runbook; a killed scheduler or failed sync produces a notification; #351/#352 fixed and regression-tested; PRs are gated by contract-check + unit + acceptance smoke; Plaid production application submitted.

### Phase 1 — M14 · Milestone A: Spending Analysis GA

*Goal: transactions flow in on their own, get categorized without user effort, and the spending views are deep enough to answer "where does my money go".*

1. **A1 — Reliable ingestion:**
   - **Scheduled background sync** per `plaid_item` (daily + jittered, via the Phase-0 job runner). Polling, not webhooks — deliberate: the Tailscale-private deployment can't (and shouldn't) expose a public webhook endpoint. Document as ADR.
   - **Re-auth flow:** detect `ITEM_LOGIN_REQUIRED`/`PENDING_EXPIRATION` from sync errors → item status `reauth_required` → Settings banner → Plaid Link **update mode** → resume. This is the #1 real-world reliability failure for any Plaid app.
   - Sync-health UX: last-synced age on Accounts/Insights (staleness plumbing from M12 already exists), DLQ badge already shipped — add notification hook (Phase 0 notifier) when an item errors.
   - **#195**: sweep the Plaid PFC taxonomy CSV into `plaid_category_map` (≥95% sandbox coverage; production institutions will lean on it harder).
2. **A2 — AI auto-categorization** (new capability, ADR required):
   - Add a `TransactionCategorizer` capability to `service/ai` mirroring the `DocumentExtractor` seam (ADR-0019 pattern): provider-pluggable (Claude/OpenAI-compatible/Ollama), so local-only users keep zero egress.
   - Resolution order becomes: manual > rule > **AI** > plaid_default > uncategorized. AI never overrides manual or rule; batch pass runs post-import over uncategorized rows only.
   - Send only `description_clean`/`merchant_name`/`amount`/category list — same PII ban as the advisor context builder; enforce with a `noimport`-style test.
   - Confidence threshold → below it, rows keep the existing "Needs review" flow (M9 #228 UI reused as-is).
   - **Cache + promote:** memoize merchant→category verdicts per user; offer "make this a rule" so repeated AI answers harden into free deterministic rules (cost control, E5). Add per-instance daily AI-call budget.
3. **A3 — Analysis depth** (design pass against the wireframe first; file issues per band):
   - Month-over-month category trends, top merchants, income vs. spending trend, average-by-category vs. this month.
   - Recurring/subscription detection (deterministic: same merchant + cadence + amount tolerance) — read-only "Recurring" band, no invented transactions (M10 invariant).
   - Mobile fixes #190 (+#192 if the advisor stays routed).

**Done when:** a production-institution item syncs daily unattended for 2+ weeks including one forced re-auth; ≥90% of new transactions auto-categorized (map+rules+AI combined) with AI never touching manual picks; Insights answers trend/merchant/recurring questions; #351 regression suite green.

### Phase 2 — M15 · Milestone B: Combined Asset View GA

*Goal: net worth you can defend — every number traceable to positions × prices, every unexplained delta visible.*

1. **B1 — Balance/position ingestion reliability:** extend Phase-1 scheduled sync to accounts + holdings (engine already reconciles on sync); verify liability accounts (credit cards, loans) reconcile with correct sign; per-account "as of" provenance.
2. **B2 — Reconciliation surfacing (the accounting story):**
   - Per-account **Reconciliation view**: balance-observation history (source, as-of), the fold vs. reported series, and every `opening_balance`/`adjustment` row with the observation that caused it (already linked by construction).
   - **Unexplained-delta flag:** an adjustment above a threshold (absolute or % of balance) marks the account "needs attention" — the accounting-best-practice framing: adjustments are suspense entries to be explained (missed statement, forgotten transfer), not silently absorbed.
   - Transfer matching pass (opposite amounts, ±N days, cross-account) proposing `is_transfer` pairs — biggest source of both spending noise (A) and reconciliation deltas (B).
   - Document the model (ADR-0017 addendum or user-facing doc): fold = facts, observations = checkpoints, adjustments = explicit residuals.
3. **B3 — Complete valuation:**
   - **Equity/ETF price provider** (ADR-0014 next phase; pick a keyless-friendly default e.g. Stooq/Yahoo, Polygon opt-in with key) behind the existing provider seam + daily scheduled refresh.
   - **Tier-1 manual price entry** (finish #352 properly): asset-level "set price" endpoint + UI, `source='manual'`.
   - **Historical net worth** from observation + price history (currently trend quality depends on txn history alone); date-range selector; per-asset-class drill-in.

**Done when:** mixed cash/brokerage/crypto/multi-currency portfolio shows complete (unflagged) net worth after scheduled refresh with zero manual re-import; every adjustment is inspectable with source observation; a deliberately skipped statement shows up as a flagged unexplained delta, not a silent number change.

### Phase 3 — M16 · Milestone C: Statement Import GA

*Goal: a non-Plaid institution is a first-class citizen: one PDF a month keeps accounts, balances, and positions current.*

1. **Positions & balances extraction** (extend ADR-0019): teach extractors a `ParsedPosition[]` + statement-balance output alongside `ParsedRow[]`; same trust boundary — AI proposes, deterministic code re-validates; commit path calls the existing `ReconcilePosition` so an imported statement balance reconciles exactly like a Plaid-reported one. Preview shows txns + positions + resulting adjustments before commit.
2. **Local extraction option — #336:** Ollama vision extractor, image-only first (photos work; PDF rasterization documented as Claude-only for now), so "zero cloud egress" users get the feature.
3. **Import ergonomics:** per-account import history with doc-total reconciliation results (matched/±delta); duplicate-statement detection (content hash already the `external_id` basis); #337 staging purge (landed in Phase 0 scheduling).

**Done when:** a brokerage PDF from an unsupported institution yields transactions + positions + a reconciled balance through preview→commit; re-importing the same PDF is a no-op; an Ollama-only user can import statement photos with zero egress.

### Phase 4 — M17 · Milestones D + E + F: Family & Plans GA

*These are ~85–90% built; this phase is verification, hardening, and small UX gaps — run after Phase 0, in parallel with 1–3 where convenient.*

1. **D — Family:**
   - Promote QA suites 6 (household privacy) and 7 (lifecycle: leave/grace/rejoin/purge/last-owner) to required CI — the privacy claims deserve mechanical proof, especially now that purge actually runs on a schedule (Phase 0).
   - Invite UX: copyable invite *link* (not just token), expiry surfaced; optional email delivery stays out of scope (no SMTP dependency for self-hosters).
   - Re-verify per-user isolation of Plaid items/ingestion under concurrent multi-user sync (each user already owns items/imports; add a two-user race test).
   - Household Insights parity check against personal (allocation/net-worth bands honor `balance_only` vs `balance_and_txns` correctly — extend aggregator privacy tests).
2. **E — Personal budgets & goals:** verify `rollover` behaves (or cut the column — it predates ADR-0018); over/near-budget notification via the Phase-0 notifier; goal contribution history view if missing.
3. **F — Family budgets & goals:** shared pace/progress already aggregator-backed; add per-member contribution attribution where visibility permits; over-budget alert honoring `balance_only` exclusions; acceptance coverage.

**Done when:** the QA.md suite list 1–9 is green in CI (cold-start suite stays opt-in); a two-user family runs both books + shared budget/goals for a full statement cycle with correct aggregates and no privacy-test regressions.

---

## Part 3 — Sequencing, risks, and what NOT to do

**Order:** Phase 0 strictly first (it changes the risk of everything else), then 1 → 2 → 3; Phase 4 is parallelizable after Phase 0. Within Phase 0, start the Plaid production application on day one — it's a calendar dependency, not an engineering one.

**Top risks:**

1. **Plaid production access lead time / requirements** (security review, OAuth per-institution). Mitigation: apply first; everything else works against sandbox meanwhile.
2. **AI categorization cost & quality drift** — mitigated by cache→rule promotion, budgets, and never letting AI outrank rules/manual.
3. **Equity price source stability** — free feeds are flaky; the provider seam + staleness flags (already shipped) mean a dead feed degrades to "stale-flagged", never to wrong numbers. Keep manual Tier-1 as the floor.
4. **Backup/restore under-tested** — mitigated by the scripted restore check; treat an unrestorable backup as a P0.

**Explicitly out of scope for production-readiness** (unchanged from ROADMAP): M11 visual hi-fi pass, AI advisor un-deferral (chat surface stays unrouted; the *provider infra* is reused by A2/C), forecast features, and any multi-instance/high-availability work — one household per instance over Tailscale is the design point (ADR-0016), not a scaling problem to solve.
