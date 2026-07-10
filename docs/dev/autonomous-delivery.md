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

Driven by the `/loop` skill in self-paced (no-interval) mode. Each iteration:

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

## Delivery order — Milestone B (Epic #385 / M15)

Correctness prerequisites first (Phase-0 trust bugs that poison B's numbers),
then the self-contained B issues, deferring the one that needs infra we don't
have yet.

1. **#351** (M13, trivial) — add `AND kind='flow'` to the dashboard by-category
   query + regression test. *Fable/Sonnet.*
2. **#352** (M13, small) — write manual/CSV trade prices as `prices` rows
   (`source='trade'`) so equities stop valuing at $0. *Sonnet.*
3. **#372** — equity/ETF price provider behind the ADR-0014 seam (keyless
   default) + manual-refresh wiring. *Sonnet; Opus picks the provider + ADR note.*
4. **#373** — Tier-1 manual price entry endpoint + UI (`source='manual'`). *Sonnet.*
5. **#370** — per-account reconciliation view + unexplained-delta "needs
   attention" flag (+ small acknowledge column, ADR-0017 addendum). *Opus design + Sonnet impl.*
6. **#371** — transfer-matching pass proposing `is_transfer` pairs. *Sonnet.*
7. **#374** — historical net worth from observation + price history; date range;
   asset-class drill-in (needs #372/#373 price history first). *Sonnet.*

**Deferred out of this window:** **#369** (scheduled balance/holdings sync) depends
on the M13 job runner (#359) and M14 background sync (#363). B's done-criteria are
demonstrable through the manual "Refresh prices" path meanwhile; true daily
scheduling lands when Phase-0/1 infra exists.

**B is "shippable" when** #351, #352, #372, #373, #370, #371, #374 are merged and a
mixed cash/brokerage/crypto/multi-currency portfolio shows a complete, unflagged
net worth after a manual refresh, with every adjustment inspectable and a skipped
statement surfacing as a flagged unexplained delta.

## After B

Continue with the epic order in `docs/PRODUCTION-READINESS.md`: Phase 0 (M13)
backup/restore + migration-safety + CI gates are the real production blockers and
should interleave; Phase 1 (M14) unblocks #369; Phases 3–4 (M16/M17) follow.
