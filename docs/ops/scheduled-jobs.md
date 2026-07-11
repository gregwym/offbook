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
| `ingestion-jobs purge` | daily *(lands with #337)* | Purges stale, uncommitted AI-import staging jobs. |

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

A failure also emits a distinct alert line (and, once the M13 notifier #360 is
wired, a real notification):

```
[job][ALERT] offbook job failed: household-purge: purge: <error>
```

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

`household-purge` also has a standalone CLI for an out-of-band run (the in-app
job doesn't replace it):

```sh
cd backend && go run ./cmd/household-purge
```

## Failure handling

A job failure is logged, alerted through the `Notifier` seam, and — because
panics are recovered per run — never crashes the server or the other jobs. Wire
real alerting by injecting the M13 notifier (#360) in place of
`jobs.LogNotifier` in `cmd/server/main.go`.
