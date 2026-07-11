# ADR-0020: In-app job runner for periodic maintenance

Status: Accepted (M13 / #359)

## Context

Offbook has periodic background work that must run without the instance owner
remembering a CLI:

- **Grace-period household purge** (`cmd/household-purge`, ADR-0007). Purging a
  former member's data links after their grace window is a *privacy promise*.
  The runner existed but nothing invoked it — a quiet contract violation.
- **Ingestion-jobs purge** (#337) — stale, uncommitted AI-import staging jobs.
- **Price refresh** (#338 Phase 3) — already ran in-app via a bespoke
  `prices.Scheduler` goroutine, the only scheduled job on the instance.

Each new periodic need was becoming ad hoc (gap E3 in
`docs/PRODUCTION-READINESS.md`). We need one mechanism.

Two options (from #359):

1. **Generalize the in-app scheduler** into a small job runner: a job is a
   name + interval + func; one runner tickers them all, logs outcomes, and
   alerts on failure.
2. **Per-FLAVOR systemd timers**, mirroring `infra/auto-deploy` and
   `infra/backup` — one unit per job, surviving app crashes.

## Decision

**Generalize the in-app scheduler** (option 1). New package
`internal/service/jobs`: `Runner` + `Job{Name, Interval, InitialDelay, Run}` +
a `Notifier` seam. `cmd/server/main.go` builds one runner and registers
`price-refresh` and `household-purge` (both daily); #337 registers its purge the
same way when it lands.

Rationale:

- The server is one long-lived process per instance that **already holds the DB
  handle and config** (`config.Load`). An in-app job reuses them directly; a
  systemd timer would re-open the DB and re-resolve config per run.
- It **matches the existing pattern** (`prices.Scheduler`) rather than
  introducing a second operational surface. The tech note in #359 explicitly
  biases to in-app for self-host simplicity.
- **One place to look**: every job logs with a `[job]` prefix and alerts through
  one `Notifier`. Systemd timers would scatter status across N units.

Accepted trade-off: **in-app jobs don't run when the process is down.** For
these tasks that's fine — purge is idempotent and time-based (a missed daily run
is caught by the next one; the aggregator already filters expired members lazily
on read, so correctness never depends on the purge having run *on time*). Work
whose *durability across crashes* matters (backups) stays on systemd timers
(`infra/backup`), where survival of an app crash is the whole point.

## Consequences

- `jobs.Runner` runs each job on its own goroutine: `InitialDelay`, then every
  `Interval`, until the server context is canceled (clean shutdown).
- **Panics are recovered** per run — one bad job can neither crash the server nor
  starve its siblings; a panic is treated as a failure (logged + alerted).
- **Failure alerting is a seam.** `jobs.Notifier` is injected; today main wires
  `jobs.LogNotifier` (logs `[job][ALERT] …`). The M13 notifier (#360) replaces
  that one injection with real delivery (ntfy/webhook/email) — no other change.
- **Observability** is the structured `[job]` log line per run (outcome +
  duration). See `docs/ops/scheduled-jobs.md` for how to read it. A durable
  `job_runs` table/endpoint is deliberately out of scope (not a job-queue system).
- `prices.Scheduler` keeps its `RunOnce` (still unit-tested); its `Start` loop is
  superseded by the runner and left as a thin, unused helper.

## Alternatives considered

- **Systemd timers per job** — better crash durability, but a second operational
  surface and per-run DB/config setup for tasks that don't need either. Reserved
  for durability-critical work (backups).
- **A real job queue** (River, asynq, etc.) — overkill for daily idempotent
  maintenance on a single-process self-hosted instance; explicitly out of scope.
