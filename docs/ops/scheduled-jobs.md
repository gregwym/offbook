# Scheduled Jobs

Offbook runs periodic maintenance in-app via a single job runner
(`internal/service/jobs`, [ADR-0020](../ADR/0020-in-app-job-runner.md)). This is
separate from the **backup** timer (systemd, `infra/backup`) and the
**auto-deploy** timer (systemd, `infra/auto-deploy`), which are host-level and
must survive an app crash.

## Jobs that run in-app

| Job | Interval | What it does |
|---|---|---|
| `household-purge` | daily | Seals in-grace member rows whose grace window elapsed and removes their `account_shares` (privacy promise, ADR-0007). Idempotent. |
| `price-refresh` | daily | Refreshes prices/FX for users who opted in via Settings (ADR-0014 §3). |
| `ingestion-jobs-purge` | daily | Reclaims abandoned AI-import staging payloads: an `ingestion_jobs` row left at `status='extracted'` past the retention window (7 days) has its staged `extraction` JSONB nulled and moves to a terminal `failed` state. The audit row survives (append-only). |
| `disk-space-check` | every 6h | Alerts when free disk space on `DISK_CHECK_PATH` (default `/`) drops below `LOW_DISK_THRESHOLD_PCT` (default 10%). |

Each runs ~1 minute after boot, then every 24h, and stops on clean shutdown.

## How to see when a job last ran

Every run logs one line with a `[job]` prefix — outcome and duration on success,
`FAILED` with the error on failure:

```
[job] household-purge: ok in 12ms — purged 2 members, removed 3 account_shares
[job] household-purge: ok in 8ms — nothing to purge
[job] price-refresh: ok in 1.4s — refresh pass complete
[job] household-purge: FAILED after 40ms: purge: <error>
```

A failure also emits a distinct alert line and, when a notifier is configured
(#360, see `docs/ops/monitoring.md`), a real push notification via ntfy and/or
a generic webhook:

```
[job][ALERT] offbook job failed: household-purge: purge: <error>
```

Set `NOTIFY_NTFY_URL` and/or `NOTIFY_WEBHOOK_URL` to enable real delivery;
neither set means alerts are logged only. Alerts are throttled per-subject
(`NOTIFY_THROTTLE_MINUTES`, default 360) so a repeatedly failing job pages once
per window, not every run.

### Reading the logs

- **Docker / compose deploy** (dev or prod flavor):

  ```sh
  docker compose -p offbook      logs backend | grep '\[job\]'   # dev
  docker compose -p offbook-prod logs backend | grep '\[job\]'   # prod
  # follow live:
  docker compose -p offbook-prod logs -f backend | grep --line-buffered '\[job\]'
  ```

- **Just the failures/alerts:**

  ```sh
  docker compose -p offbook-prod logs backend | grep '\[job\]\[ALERT\]'
  ```

- **Running the server directly** (`make dev`): the same lines go to
  `/tmp/offbook-server.log` (`command make logs`).

## Running a job manually

Two jobs also have standalone CLIs for an out-of-band run (the in-app jobs don't
replace them):

```sh
cd backend && go run ./cmd/household-purge

# AI-staging purge — dry-run by default; --apply to execute:
cd backend && go run ./cmd/ingestion-jobs-purge                     # dry run
cd backend && go run ./cmd/ingestion-jobs-purge --apply             # purge
cd backend && go run ./cmd/ingestion-jobs-purge --retention-days 14 # custom window
```

## Failure handling

A job failure is logged, alerted through the `Notifier` seam, and — because
panics are recovered per run — never crashes the server or the other jobs. The
M13 notifier (#360) is wired in `cmd/server/main.go` via
`internal/service/notify.Build(cfg, log.Printf)`, replacing `jobs.LogNotifier`;
see `docs/ops/monitoring.md` for the full alerting surface (job failures,
Plaid item errors, backup/deploy failures, low disk) and env vars.
