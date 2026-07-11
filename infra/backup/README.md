# infra/backup

Nightly `pg_dump` backups with retention and automated restore-verification for
each Offbook instance. User-facing docs — how to back up, restore, schedule, and
recover — live in **[docs/ops/backup-restore.md](../../docs/ops/backup-restore.md)**.
This file is the implementation map.

## Pieces

| File | Role |
|---|---|
| `lib.sh` | Sourced helpers. Parses `$OFFBOOK_COMPOSE` into the compose invocation and runs commands inside the `postgres` container (`pg_sh`, `db_name`, `db_user`, `ensure_postgres_up`). |
| `backup.sh` | Dump the live DB to `$BACKUP_DIR` (`pg_dump -Fc`), then prune + optional off-host copy. `command make backup`. |
| `prune.sh` | Retention only (pure filesystem; no DB). Keeps N dailies + one dump per week for M weeks. Independently testable. |
| `offhost.sh` | Opt-in off-host copy seam (rclone/restic via `$BACKUP_REMOTE`). No-op when unset. |
| `restore.sh` | Drop + recreate the live DB and `pg_restore` a chosen dump. `command make restore BACKUP=…` (the Makefile adds the confirm prompt). |
| `verify.sh` | Restore the latest dump into a throwaway scratch DB and sanity-check it, then drop it. Never touches the live DB. `command make backup-verify`. |
| `run.sh` | Nightly entrypoint the timer fires: `make backup` then `make backup-verify`, with a notifier-on-failure seam (`$BACKUP_NOTIFY_CMD`, for #360). |
| `offbook-backup@.{service,timer}` | User-level templated systemd units (instance name = FLAVOR). Rendered/installed by `install.sh`. |
| `install.sh` / `uninstall.sh` | Manage the per-FLAVOR timer. `command make backup-install [FLAVOR=prod]`. |

## Design notes

- **Convention-agnostic:** the scripts never re-derive the compose invocation.
  The Makefile owns it (it varies by `FLAVOR`) and passes it in as
  `$OFFBOOK_COMPOSE`; the scripts word-split it into a command array. Point
  `OFFBOOK_COMPOSE` at any compose project with a `postgres` service and the
  scripts work — which is exactly how they're tested against a throwaway stack.
- **Container-side DB ops:** dumps/restores run inside the `postgres` container
  via `docker compose exec`, so they always use the container's own
  `$POSTGRES_USER`/`$POSTGRES_DB` and a client binary matching the server — no
  host `psql` install, no hard-coded DB name.
- **Portable bash:** no `mapfile` / associative arrays, so the scripts run on the
  Linux deployment hosts *and* a macOS (bash 3.2) dev box. `prune.sh`'s ISO-week
  bucketing uses GNU `date -d` and degrades to "keep newest N+M" without it.
- **Mirrors `infra/auto-deploy`:** same templated-unit + install-script pattern,
  same single-flight `flock`, same "call back into `make`" approach — so the two
  timers are operated identically.
