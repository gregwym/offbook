# Backup & Restore Runbook

Offbook keeps every account, transaction, budget, and household in a single
Postgres database. Losing that volume — disk failure, a bad migration, a
fat-fingered `make teardown` — loses everything unless there's a backup. This
runbook is the recovery path: **you should be able to restore last night's state
by following these steps alone, no improvisation.**

Scope: nightly logical backups (`pg_dump`) with retention and automated
restore-verification. Out of scope (future options, see bottom): WAL archiving /
point-in-time recovery, multi-host HA.

## What the backup is

- A per-instance `pg_dump -Fc` (custom format: compressed, restorable with
  `pg_restore`) of the instance's database.
- Written to `backups/<project>/` on the **host**, deliberately **outside** the
  Postgres data volume — so losing the volume never loses the backups with it.
- File name: `<project>-YYYYmmdd-HHMMSS.dump` (e.g. `offbook-prod-20260710-023012.dump`).
- `<project>` is the compose project for the flavor: `offbook` (dev) or
  `offbook-prod` (prod). Dev and prod back up to separate directories.

## Taking a backup

```sh
command make backup                # dev instance (project offbook)
command make backup FLAVOR=prod    # prod instance (project offbook-prod)
```

This dumps the DB, prunes old dumps per the retention policy, and — if an
off-host target is configured — copies the new dump off-host. It requires the
instance's Postgres to be running (`command make deploy` first).

`command make deploy` also takes a backup **automatically before it runs**, so
every update of a data-bearing instance is preceded by a fresh dump (migration
safety, [AGENTS.md § Migration Safety](../../AGENTS.md)). First boot has no data
yet, so that pre-deploy backup is skipped cleanly.

List what's on hand:

```sh
command make backup-list                 # dev
command make backup-list FLAVOR=prod     # prod
```

## Nightly schedule

Install a user-level systemd timer (mirrors the auto-deploy timer — no sudo, no
editing checked-in files):

```sh
command make backup-install              # dev,  offbook-backup@dev.timer
command make backup-install FLAVOR=prod  # prod, offbook-backup@prod.timer
```

It fires nightly at ~02:30 (randomized delay so multiple instances don't dump at
once), runs `make backup` then `make backup-verify`, and is `Persistent=true`
(a missed run catches up after the host wakes). Inspect it:

```sh
systemctl --user list-timers 'offbook-backup@*'
journalctl --user -u offbook-backup@prod -f
systemctl --user start offbook-backup@prod.service   # run now
```

Remove it with `command make backup-uninstall [FLAVOR=prod]`.

On a host without systemd, run `command make backup` from cron instead.

## Retention

Grandfather-father-son: keep the newest **N dailies** plus one dump per ISO week
for the most-recent **M weeks**. Defaults: `BACKUP_KEEP_DAILY=7`,
`BACKUP_KEEP_WEEKLY=4` (≈ a month of history in ~11 dumps). Override in the
instance env file (`.env` / `.env.prod`) or on the command line:

```sh
command make backup BACKUP_KEEP_DAILY=14 BACKUP_KEEP_WEEKLY=8
```

## Restore-verification (an unrestored backup is not a backup)

```sh
command make backup-verify               # verifies the latest dev dump
command make backup-verify FLAVOR=prod   # verifies the latest prod dump
```

This restores the **latest** dump into a throwaway scratch database
(`<db>_restore_check`), asserts the schema and seed data survived (a
`schema_migrations` version exists and isn't dirty; ≥ 20 system categories), then
drops the scratch DB. It **never touches the live database**, so it is safe to
run on a schedule — the nightly timer runs it after every backup.

## Restoring (disaster recovery)

**This is destructive**: it drops and recreates the live database, replacing all
current data with the dump's contents. Do it deliberately.

1. Pick the dump to restore:

   ```sh
   command make backup-list FLAVOR=prod
   ```

2. (Recommended) stop the app so nothing races the restore — Postgres can stay
   up; the restore terminates stray connections itself:

   ```sh
   command make down FLAVOR=prod     # or leave the stack up; see note below
   ```

   If you `down` the whole stack, bring **only Postgres** back up before
   restoring, or skip `down` and restore against the running stack (the backend
   will error until you redeploy in step 4 — that's fine).

3. Restore. You'll be asked to type the project name to confirm (skip with
   `FORCE=1` in automation):

   ```sh
   command make restore FLAVOR=prod BACKUP=backups/offbook-prod/offbook-prod-20260710-023012.dump
   ```

4. Restart the backend so it reconnects to the fresh database:

   ```sh
   command make deploy FLAVOR=prod
   ```

5. Sanity-check: open the app, confirm accounts / transactions / balances look
   like last night. `GET /api/v1/health` should be `ok`.

### Recovering from a *lost volume*

If the Postgres volume itself is gone (disk failure, `make teardown`):

1. Recreate the stack — `command make deploy FLAVOR=prod` brings up a fresh,
   empty, migrated database.
2. Run the restore (steps 3–4 above) with your latest dump.
3. If you keep off-host copies and the local `backups/` dir was on the lost
   disk, pull the dump back from the off-host target first (see below).

## Off-host copies (optional, opt-in)

An on-host backup survives a lost volume but **not** total host loss. To close
that gap, set `BACKUP_REMOTE` in the instance env file to an rclone or restic
target; each `make backup` then copies the new dump off-host. Unset ⇒ on-host
only (the default).

```sh
# .env.prod
BACKUP_REMOTE=rclone:myremote:offbook-backups   # rclone remote:path
# or
BACKUP_REMOTE=restic:/srv/restic-repo           # restic repository
```

The remote and its credentials are yours to provision — Offbook never invents a
target. `rclone` reads its own config; `restic` reads `RESTIC_PASSWORD` etc. from
the environment. Restore from off-host by pulling the `.dump` back into
`backups/<project>/` (`rclone copy` / `restic restore`) and running
`command make restore BACKUP=<file>` as usual.

## Failure notifications

`make backup` / the nightly runner exit non-zero on any failure, so systemd
records the failure in the journal. Backup/verify failure is also wired to the
M13 notifier via a seam: set `BACKUP_NOTIFY_CMD` to a command that takes the
message as `$1` and it fires on failure. When the notifier (#360) lands, point
`BACKUP_NOTIFY_CMD` at it (or replace the seam body in
`infra/backup/run.sh`) — no other change needed.

## Future options (not implemented)

- **Point-in-time recovery (WAL archiving):** continuous WAL shipping +
  base backups for recovery to an arbitrary second, not just the last nightly
  dump. Warranted once RPO needs to drop below ~24h.
- **Multi-host HA / streaming replication:** a warm standby for failover.
  Out of scope for the single-host Tailscale deployment model (ADR-0016).
