# Monitoring & Alerting

Offbook is designed to run unattended on a Tailscale-private host. Without a
watcher, a failure — a stuck scheduler, a Plaid item that stopped syncing, a
failed backup, a full disk — surfaces only as stale numbers, weeks later. This
page covers the two halves of the fix: an internal `Notifier` seam that pages
the instance owner when something breaks, and an external uptime check for
when the process itself goes dark.

## The `Notifier` seam

`internal/service/notify` (`#360`) implements a small interface used by every
Go entry point:

```go
type Notifier interface {
    Notify(ctx context.Context, subject, detail string)
}
```

`notify.Build(cfg, log.Printf)` returns a `Notifier` wired from config:

- **ntfy** (`NOTIFY_NTFY_URL`) — a plain POST to an [ntfy](https://ntfy.sh)
  topic URL (ntfy.sh or self-hosted). `Title` header = subject, body = detail.
- **Generic webhook** (`NOTIFY_WEBHOOK_URL`) — a POST of
  `{"subject","detail","time"}` as JSON to any HTTP endpoint. Self-host
  friendly; no vendor-specific payload shape.
- Both may be set at once (fan-out); neither being set falls back to a
  **log-only** notifier, so a fresh instance with no monitoring configured
  still surfaces failures in the logs rather than silently.

Every delivery is wrapped in `Throttled`, which dedupes repeat alerts for the
same subject string within a window (`NOTIFY_THROTTLE_MINUTES`, default 360 =
6h) — an erroring Plaid item or a stuck scheduler pages once per window, not
every run.

Shell-driven infra (`infra/backup/*.sh`, `infra/auto-deploy/auto-deploy.sh`,
which run outside the Go process) uses `infra/notify/notify.sh "<subject>"
"<detail>"`, a small POSIX helper that reads the same `NOTIFY_NTFY_URL` /
`NOTIFY_WEBHOOK_URL` env vars so shell and in-app alerts share one config and
one set of delivery targets. It is a no-op (exit 0) when neither var is set —
a missing notifier config must never be the reason a backup or deploy script
fails.

## Config

Set in `.env` (dev) or `.env.prod` (prod flavor) — see `.env.example`:

| Var | Default | Meaning |
|---|---|---|
| `NOTIFY_NTFY_URL` | unset | ntfy topic URL. Unset = ntfy delivery disabled. |
| `NOTIFY_WEBHOOK_URL` | unset | Generic JSON webhook URL. Unset = webhook delivery disabled. |
| `NOTIFY_THROTTLE_MINUTES` | `360` | Per-subject throttle window in minutes. |
| `LOW_DISK_THRESHOLD_PCT` | `10.0` | Free-space percentage below which the `disk-space-check` job alerts. |
| `DISK_CHECK_PATH` | `/` | Filesystem path the `disk-space-check` job measures. |

None of these are fatal to boot if malformed — an unparseable value falls back
to its default rather than blocking startup (monitoring config must never be
the reason the app won't start).

## Wired events

| Event | Source | Subject prefix |
|---|---|---|
| Scheduled job failure (`household-purge`, `price-refresh`, `ingestion-jobs-purge`, `disk-space-check`) | `internal/service/jobs.Runner`, via `notify.Build` in `cmd/server/main.go` | `offbook job failed: <job>` |
| Plaid item sync enters `error` | `internal/service/plaid.Service.WithNotifier`, wired in `internal/router/router.go` | `Plaid item sync error: <plaid_item_id>` |
| Backup failure (`pg_dump` fails or produces an empty file) | `infra/backup/backup.sh` → `infra/notify/notify.sh` | `offbook backup failed (<project>)` |
| Backup job failure (systemd timer path, any step) | `infra/backup/run.sh` → `infra/notify/notify.sh` (or `BACKUP_NOTIFY_CMD` override) | `offbook backup[<flavor>] failed` |
| Deploy failure (`make deploy` non-zero exit) | `infra/auto-deploy/auto-deploy.sh` → `infra/notify/notify.sh` | `offbook deploy failed (<flavor>)` |
| Low disk space | `disk-space-check` scheduled job (`cmd/server/main.go`) | `offbook job failed: disk-space-check` |

Plaid item `reauth_required` alerting is deferred to `#364` (M14), which adds
the re-auth flow this seam will hook into — `plaidsvc.Notifier` is already
shaped to take it without a signature change.

See [`docs/ops/scheduled-jobs.md`](scheduled-jobs.md) for the job runner
itself and [`docs/ops/backup-restore.md`](backup-restore.md) for the backup
system.

## External uptime check

The `Notifier` seam only fires from *inside* a running process — if the
backend itself crashes, hangs, or the host loses power, nothing in-app can
alert you. Point an external uptime checker at `/api/v1/health` over your
Tailscale network (the host is Tailscale-private, so the checker needs
network access to it — e.g. run the checker on another Tailscale node, or use
a provider that supports checking via a Tailscale [Funnel](https://tailscale.com/kb/1223/funnel)
or a self-hosted poller):

```
GET http://<magicdns-hostname>/api/v1/health
```

- `200` with `{"data":{"status":"ok"}}` — healthy.
- Any other status, timeout, or connection failure — page.

Recommended check interval: 5 minutes. This is deliberately out of scope for
Offbook itself to implement (no bundled uptime-monitor service) — self-hosted
options include [Uptime Kuma](https://github.com/louislam/uptime-kuma) (can
notify via the same ntfy/webhook targets configured above) or any external SaaS
uptime checker that can reach the Tailscale network.

## Out of scope

Metrics/Prometheus/Grafana, log shipping, and email provider integrations
beyond the generic webhook are explicitly out of scope for `#360` — see the
issue for rationale. Structured JSON logs (`internal/logging`) are designed to
be easy to ship to an external aggregator later without a rewrite, but Offbook
does not ship a log-shipping pipeline itself.
