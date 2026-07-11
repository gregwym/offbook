# Autonomous Delivery Harness — Production-Readiness Plan

Durable definition of the self-paced loop that delivers
[`docs/PRODUCTION-READINESS.md`](../PRODUCTION-READINESS.md) (milestones M13–M17).
This file is the source of truth an autonomous session re-reads on wake-up so it
can resume without re-deriving strategy. It exists because the plan is too large
for one quota window and must survive context resets.

## Operating constraints

- **Single Claude Pro plan.** Quota is the governor, not wall-clock. Three pools:
  a rolling 5-hour cap (all models), a weekly all-models cap, and a **separate
  weekly Fable cap**. When a pool is exhausted the loop pauses and resumes in the
  next window — that is expected, not a failure.
- **`main` is protected.** Every change lands via feature branch → PR → squash-merge.
  Self-merge is pre-authorized for the autonomous workflow (AGENTS.md § Git Discipline).
- **Never `--amend` / `reset --hard` / force-push.** Fix forward.

## Model tiering (leverage models wisely)

Pick the cheapest model that can do the task correctly. Delegate via the Agent
tool's `model` override; keep the Opus orchestrator's own token burn low.

| Tier | Model | Use for |
|---|---|---|
| Orchestrator | **Opus** (main session) | Sequencing, dependency calls, ADR/design decisions, merge gate, verification interpretation. Sparingly. |
| Workhorse | **Sonnet** subagent | Full-stack implementation of one issue: handler→service→repo + frontend + tests. |
| Mechanical | **Fable** subagent | One-line/boilerplate fixes, docs, fixtures, seed data. Leans on the separate weekly Fable pool that has the most headroom. |
| Search | **Explore** / **Haiku** | Code search, context gathering, "where is X" — never carries implementation. |

Rule of thumb: if a task needs judgment about correctness of money/valuation code,
it is Sonnet or Opus, never Fable.

## The loop (one iteration = one shipped PR)

The durable driver is a local launchd timer running headless `claude -p` (see
**Durability** below), not the interactive `/loop` skill — but each firing runs
the same iteration. Each iteration:

1. **Pick** the next unchecked issue from the active epic in dependency order
   (below). Skip issues whose prerequisites are unmerged.
2. **Branch** `feature/{issue}-{slug}` off latest `origin/main` (always re-fetch;
   earlier PRs in the run have moved main).
3. **Delegate** implementation to a subagent at the tier the issue warrants. The
   subagent implements to the issue's acceptance criteria + Product Goal, adds
   tests, and runs `make verify` + `make schema-check` from `backend/`.
4. **Gate** — orchestrator confirms `make verify` green locally, pushes, opens the
   PR (`Closes #N`), waits for CI green (`gh pr checks --watch`), then
   `gh pr merge --squash --delete-branch`.
5. **Tick** the epic checklist and advance. On failure, file findings as a comment,
   leave the PR open, and move to the next independent issue rather than blocking.

Pacing: the loop is quota-bound, not time-bound. Use a long fallback wake
(1200s+) only as a heartbeat; the real signal is subagent completion.

## Durability — the engine is a local launchd timer, not a live session

**The failures this section fixes.** (1) An interactive Claude session driving
the loop dies when the 5-hour quota cap is hit (or the REPL is closed) —
`ScheduleWakeup`/`CronCreate` only fire while that session is alive, so the loop
silently stops. (2) The claude.ai **cloud-routine** experiment (July 2026) fixed
the survival problem and did ship five issues, but each firing pushed
interactive permission approvals to the owner's phone (not autonomous) and its
sessions were unobservable in the Claude app.

**The engine:** a user-level **macOS LaunchAgent** (`com.offbook.delivery`,
managed by `command make delivery-install` / `delivery-uninstall`; implementation
in [`infra/delivery-loop/`](../../infra/delivery-loop/README.md)). Every ~3 h it
runs one headless iteration — `claude -p` with the prompt in
`infra/delivery-loop/iteration-prompt.md`, Sonnet, `--permission-mode auto`
(classifier-gated auto mode — never `--dangerously-skip-permissions`, owner
decision) so nothing ever waits on a human — inside a dedicated delivery clone
(`~/src/offbook-delivery`), never the owner's working checkout. Logs land in
`~/Library/Logs/offbook-delivery/`. A quota-capped firing fails cheaply and the
next firing — after the window resets — picks up exactly where the last left
off. That is the self-healing property: wall-clock scheduling outside any Claude
session, on the machine where git/gh/Docker are already warm.

Iteration invariants (encoded in the iteration prompt):

- **One iteration per firing.** Each firing ships *at most one* PR, then exits.
  This bounds per-firing cost and lets quota back-pressure throttle naturally —
  do not loop inside a single firing.
- **Idempotent pick.** Before starting an issue, `gh pr list --state open` and
  `git branch -a`: if a branch/PR already exists for the next issue, continue or
  merge it rather than duplicating; if that PR is green + mergeable, merge and
  advance; if open but red, fix forward; only then start a fresh issue. This is
  what keeps two overlapping firings from colliding.
- **State lives in GitHub, not the session.** The epic checklists (#383/#384/#385)
  plus merged PRs are the source of truth for "what's done." A cold firing
  reconstructs position from them + this file — never from prior session memory.
- **Cadence** ≈ every 3h (tunable at install: `OFFBOOK_DELIVERY_INTERVAL`); a
  capped firing simply retries next window. launchd persists across reboots —
  no expiry, no re-arming.
- **Model:** the iteration runs as Sonnet (the workhorse tier). Issues flagged
  Opus-design in the delivery order get their ADR/design slice written first, then
  implemented; money/valuation correctness is never downgraded to Fable.
- **Human-gated stops** (#362 and any owner-secret step) are done to their
  agent-completable slice, then the iteration comments on the issue and advances —
  never blocks the loop.

An interactive session may still hand-drive iterations (e.g. to burn a live quota
window faster), but it must not run concurrently with the timer on the *same*
issue — the idempotency guard above is the tie-breaker. The launchd timer is the
thing that guarantees the plan keeps moving when no one is watching. (The retired
cloud routine `trig_01BGrffWSrKTmYrMoTETRkgD` is kept disabled as a fallback.)

## Delivery scope — everything up to Milestone B

Owner direction: deliver the plan **in sequence up to and including Milestone B**,
i.e. the whole of **Phase 0 (M13) → Phase 1 (M14 / Milestone A) → Phase 2 (M15 /
Milestone B)**. This is the plan's strict ordering ("Phase 0 first … then 1 → 2");
it also makes #369 reachable rather than deferred, because M14 supplies the
background sync it needs. M16/M17 (C, D/E/F) are out of scope for this run.

Two issues are **human-gated** — an agent prepares them but cannot complete them
solo; do the agent-completable slice, then stop and flag the owner:
- **#362** Plaid production application — a real-world process (application,
  security questionnaire, OAuth per-institution registration). Agent deliverable =
  the application checklist/ADR + `.env.prod` wiring; actual approval is the owner's
  and has weeks of lead time. **Start its paperwork day one; never block on it.**
- Any step needing owner-only secrets/infra (off-host backup target, notifier
  endpoint URL) ships with a working default + documented config seam, not a
  fabricated credential.

### Delivery order (dependency-sequenced)

**Phase 0 — M13 (Epic #383), strictly first; every later item inherits its guarantees.**
1. **#351** — dashboard by-category `kind='flow'` filter + regression test. *(in flight, Sonnet)*
2. **#352** — write trade prices as `prices` rows (`source='trade'`); unblocks equity valuation. *Sonnet.*
3. **#358** — migration safety: expand→migrate→contract policy + up→down→up round-trip test in `make verify`. *Sonnet.* (Lands before any later migration.)
4. **#357** — backups & restore: nightly `pg_dump`, retention, `make backup`/`make restore`, scripted restore verification. *Sonnet; Opus reviews the restore-test design.*
5. **#359** — job runner (generalize `prices.Scheduler`): schedule household-purge + ingestion-jobs purge. **Prerequisite for #337, #363, #369.** *Opus design + Sonnet impl.*
6. **#337** — purge stale uncommitted AI-import staging jobs (a runner #359 schedules). *Sonnet.*
7. **#360** — monitoring & alerting: structured JSON logs + notifier seam (ntfy/webhook). **Prerequisite for #365.** *Sonnet.*
8. **#272** — CI: acceptance baseline smoke required on PR. *Fable/Sonnet.*
9. **#275** — CI: frontend unit-test setup (vitest + RTL + msw) + #266 regression. *Sonnet.*
10. **#361** — deploy/rollback runbook + post-deploy SHA smoke. *Fable draft + Opus review.*
11. **#362** — Plaid production application paperwork (human-gated; draft + flag). *Fable draft.*

**Phase 1 — M14 / Milestone A (Epic #384), needs #359 scheduler + #360 notifier.**
12. **#363** — scheduled background transaction sync per `plaid_item` (polling) + ADR. **Prerequisite for #369.** *Opus ADR + Sonnet impl.*
13. **#364** — Plaid re-auth flow: `ITEM_LOGIN_REQUIRED` → Link update mode → resume. *Sonnet.*
14. **#365** — sync-health UX (last-synced age) + notifier hook on item error (uses #360). *Sonnet.*
15. **#195** — Plaid PFC taxonomy sweep into `plaid_category_map`. *Fable.*
16. **#366** — AI auto-categorization `TransactionCategorizer` (ADR + impl; PII-banned like the advisor). *Opus ADR + Sonnet impl.*
17. **#367** — spending-analysis depth: MoM trends, top merchants, income vs. spending. *Sonnet.*
18. **#368** — deterministic recurring/subscription detection (read-only band). *Sonnet.*
19. **#190** — Rules page mobile overflow. *Fable.*
20. **#192** — AI Advisor mobile layout (only if the advisor stays routed; else skip + note). *Fable.*

**Phase 2 — M15 / Milestone B (Epic #385).**
21. **#372** — equity/ETF price provider behind the ADR-0014 seam (keyless default) + refresh wiring. *Sonnet; Opus picks provider + ADR note.*
22. **#373** — Tier-1 manual price entry endpoint + UI (`source='manual'`). *Sonnet.*
23. **#369** — extend scheduled sync (#363) to balances + holdings; liability signs; as-of provenance. *Sonnet.*
24. **#370** — per-account reconciliation view + unexplained-delta flag (+ acknowledge column, ADR-0017 addendum). *Opus design + Sonnet impl.*
25. **#371** — transfer-matching pass proposing `is_transfer` pairs. *Sonnet.*
26. **#374** — historical net worth from observation + price history; date range; asset-class drill-in. *Sonnet.*

**Up to B is "done" when** all of the above (minus the human-gated remainder of #362)
are merged, the M13/M14/M15 epic done-criteria hold, and a mixed
cash/brokerage/crypto/multi-currency portfolio syncs unattended and shows a
complete, unflagged net worth with every adjustment inspectable and a skipped
statement surfacing as a flagged unexplained delta.

## After B

M16 (Statement Import / C) and M17 (Family & Plans / D-E-F) follow the same loop;
resume from `docs/PRODUCTION-READINESS.md` Phases 3–4 when the owner green-lights.
