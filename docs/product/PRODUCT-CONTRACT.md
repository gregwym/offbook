# Offbook Product Contract — Daily Driver

Status: initial operating contract, **not** a claim that Monarch parity exists.

## Goal

**Current concrete target:** [Daily Driver v1](DAILY-DRIVER-V1.md) and [Goal #423](https://github.com/gregwym/offbook/issues/423). Optimize for this end-to-end deliverable, not Monarch feature-count parity.

Turn Offbook into a trustworthy personal finance daily driver. Prioritize correct ingestion, understandable money totals, usable transaction review, investing, budget and reports over completing an ever-growing feature list.

## Research contract
Monarch Money is a **product-behavior reference**, not a source for assumed internal architecture. For each behavior record: user job, source URL and observation date, source confidence (`verified-doc` / `verified-ui` / `unverified`), observed behavior, edge/error states, cross-feature interactions, Offbook code gap, proposed acceptance tests.

The research agent maintains `docs/product/MONARCH-BEHAVIOR-MATRIX.md` when the first research slice is complete. Never invent undocumented Monarch behavior. Do not copy proprietary code, bypass access restrictions or expose personal credentials. When unavailable, decide conservatively based on financial invariants and mark the behavior as an Offbook design decision.

## Financial invariants
- Transactions, balances, positions, prices and reconciliation adjustments are auditable facts.
- Review state, categorization, splitting, visibility, tags and reports must not silently alter account holdings or balances.
- Import retry, account resync and pending-to-posted updates are idempotent and preserve user overrides.
- Transfers, credit-card payments, refunds and trade legs do not double-count spending or net worth.
- Missing/stale prices and partial aggregates are explicitly visible; never silently coerce them to $0.
- Monetary calculations use exact decimal arithmetic.
- Simplify household UX over time, but preserve current ownership checks and PII isolation until a tested migration is implemented.
- Use **synthetic data and Plaid Sandbox only** for Cloud development and QA.

## Priorities
1. **P0 Trust**: backup/restore, safe migrations, ingestion idempotence, sync and reauth recovery, reconciliation, authorization, auditability.
2. **P1 Daily Driver**: accounts, fast transaction review and rules, usable reports, reliable investment totals, basic budgets, desktop/mobile core flows.
3. **P2 Maturity**: splits, rollover/flex budgeting, goals funding, recurring management, saved reports, simpler household navigation.
4. **P3 Later**: advanced forecasts and other nonessential Monarch parity.

Work in end-to-end vertical slices with frontend, backend, acceptance tests and error states together. Existing Roadmap checkbox status is not proof a behavior works; inspect code.

## Release gate
No open P0/P1 issues; finance invariant regressions passing; independent **batch** QA PASS for the exact release candidate SHA (not every development PR); verified backup restoration; safe migration/rollback plan; sync errors, missing prices and unexplained deltas surfaced instead of hidden. Begin a controlled owner-authorized real-data pilot, then monitor a 30-day dogfood window. Do not block the first pilot on an impossible promise of prior real-data testing.

## Product changes
An implementation issue links a sourced behavior contract or an explicitly adopted Offbook requirement. A PR states Product Goal, scenarios, test evidence, financial/security impact, and migration/rollback consequences. Developer tests are not independent QA.

A unified household workspace with a simpler private/shared model is **proposed direction**, not current behavior; approve an ADR before altering privacy semantics.
