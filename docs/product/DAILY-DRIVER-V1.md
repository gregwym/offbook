# Daily Driver v1 — active Offbook delivery goal

**Canonical tracking issue:** [#423](https://github.com/gregwym/offbook/issues/423).

This is the ONE active objective for autonomous Engineering. Re-read this charter on every fresh session. Existing roadmap items and Monarch references help define behavior, but **do not override this target or justify unrelated feature work**.

## Product outcome

A single user can sync or import a realistic month of financial activity, reconcile account balances and holdings, review/categorize transactions, see accurate spending and budgets, and inspect trustworthy net worth on desktop/mobile. Repeated syncing preserves manually curated data and does not duplicate money. Partial/missing market data is explicit instead of silently wrong.

**First deliver a reliable single-user daily driver.** Household sophistication, advanced forecasts, and perfect Monarch parity do not block this goal; keep existing multi-user privacy checks passing.

## Acceptance fixture (synthetic, incrementally build it)

At least 2 bank accounts, 1 credit card, 1 brokerage; 200+ realistic synthetic transactions; transfers, credit-card payments, refunds, split transactions, adjustments, pending→posted, duplicate statement upload, repeated cursor sync, user recategorization, trade holdings/cash and time-series prices/FX, including stale/missing inputs. Include mobile-width browser runs.

This is a target fixture, not a claim that tests already exist.

## Outcome gates

1. Cold start -> import/sandbox sync -> transaction review/categorize/split -> category spending -> monthly budget -> historical net worth, with automated desktop/mobile journey tests.
2. Retries and resyncs are idempotent; pending→posted, account corrections, manual edits and duplicates preserve exactly-once financial effects.
3. Transfers, credit-card payments, refunds, fees, splits and investments do not pollute spending or produce inconsistent net worth. Use exact decimal totals.
4. Reconciliation visibly explains balance discrepancies, and partial/stale price coverage is flagged, not valued at zero without explanation.
5. Month-level spending, budget actuals, and asset balances are internally consistent against fixtures.
6. Existing CI and invariant regressions are green; an **independent batch QA** reviews the final release SHA / merged range; no open P0/P1 findings.
7. Production release remains a separate owner-controlled manual action; Plaid Production approval is external dependency #362, not a reason for Cloud to request live credentials.

The overall goal is **not DONE** because PRs were merged; it is DONE only when these user-visible checks and release readiness are demonstrated.

## Priority queue (validate actual code before creating work)

**Priority 0 — Fix blockers and protect data:** existing #197 QA worktree/environment issue (retest; close if obsolete), deterministic fixtures, sync/import retries, transfer/refund/card-payment/spilt invariants. Record actual coverage instead of rewriting shipped features.

**Priority 1 — Daily transaction loop:** categorization and rule precedence, review, repeat sync, real monthly spending/merchant/recurring and basic budgets, mobile layout.

**Priority 2 — Net worth:** #374 historical net worth with observation and price/FX as-of, partial-data flags, drill-in.

**Priority 3 — Import fallback:** #375 positions/balance statement extraction, #376 history/reconciliation/dedup. If a prerequisite is blocking, pick the next independently shippable slice.

**Defects win:** Independent batch QA P0/P1 bugs outrank new features. Do not leave failed QA unresolved while starting more untested surfaces.

M14 #384 is implementation-checkbox complete; audit real behavior instead of assuming unfinished code. M13–M17 roadmap documents can be stale. Maintain about 3–5 scoped, actionable issues, not a backlog of speculative ideas.

## Engineering throughput policy

Use Opus 5.5 as the principal coding model **when available in the account**. No weekly PR ceiling or pre-set token budget. On each run: pick highest-impact unblocked work, complete a vertical slice (user-facing behavior + tests), run CI, squash-merge on green, update the Issue/Epic, then keep delivering if time/quota and queue permit.

Do not spend successive sessions generating architecture commentary without verified merges. Prefer deterministic tests and bug fixes over repeatedly rereading large research sources. Respect provider limits; stop cleanly when quota is exhausted, leaving a resumable Issue/PR with exact state.

## Quality and release separation

- Every PR: required CI with unit/integration/contract/migration/targeted baseline smoke and appropriate regression tests. **No per-PR AI QA requirement.**
- Batch QA: review all merges since the last accepted checkpoint (default twice weekly or at ~8 merges, whichever sooner if an additional run is available); test shared user journeys on the integrated main SHA, file P0/P1 bugs, and report explicit coverage gaps.
- Auto-merge to **main** is delegated for normal code including financial logic and compatible additive migrations **only with mandatory deterministic checks and safe staged migration rules**. No bypass for red/missing CI or known P0/P1. Irreversible/destructive steps, external purchases/credentials, or production deployment are NOT delegated.
- Production Pi remains manual, isolated from integration main; final batch QA + verified backups/restore gate a real-data release.

## Progress reporting

Track new user workflows that genuinely pass their acceptance fixture, unreconciled deltas, P0/P1 counts, merged PR links, CI outcomes, latest batch QA SHA and remaining blockers. **A week with many tokens used but no working user journey or clear diagnosis is a failure; a week with multiple verified merges is progress.**
