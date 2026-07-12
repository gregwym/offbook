# Deploy & Rollback Runbook

Every Offbook deploy is `command make deploy` — first boot, routine updates, and
rollback all go through one target (ADR-0016). This page covers the operational
side: what a normal deploy does, how to tell a deploy failed, how to roll back
to a known-good build, and how rollback interacts with backups and migrations.
See [`infra/auto-deploy/README.md`](../../infra/auto-deploy/README.md) for the
poll-and-redeploy timer this all sits under, and
[`docs/ops/backup-restore.md`](backup-restore.md) for the backup half.

## What a normal deploy does

```sh
command make deploy               # dev instance
command make deploy FLAVOR=prod   # prod instance
```

In order:

1. **`ensure-env`** — creates the instance's env file from its `.example` on
   first boot; generates `SESSION_SECRET` if missing (never rotates an
   existing one).
2. **`pre-deploy-backup`** — if Postgres is already running (i.e. this isn't
   first boot), takes a backup *before* touching anything (migration safety,
   [AGENTS.md § Migration Safety](../../AGENTS.md)). A backup failure aborts
   the deploy — don't migrate what you can't restore.
3. **Build + bring up** — `docker compose up -d --build`, stamping the image
   with `GIT_SHA=$(git rev-parse --short HEAD)` (surfaced at `GET /health` and
   Settings → About, #310). If the Tailscale sidecar is already up this only
   recreates `backend`+`frontend` (`--no-deps`); first boot brings up the full
   stack and needs `TS_AUTHKEY`/`TS_HOSTNAME` once.
4. **Prune** dangling images (keeps the disk from filling on repeated deploys).
5. **Post-deploy smoke** (#361) — polls `/health` *inside* the backend
   container (a container exec, so it works even when no host ports are
   published, i.e. prod) up to 10 times, 3s apart. Passes only when the
   response is `{"data":{"status":"ok","version":"<the SHA just deployed>"}}`.
   On failure: prints what it saw, fires a notifier event via
   `infra/notify/notify.sh` (the #360 ntfy/webhook seam — a no-op if neither
   is configured), and **`make deploy` exits non-zero**.

A deploy that only brings the containers up but never passes the smoke check
is not considered successful — the command fails loudly instead of leaving a
crash-looping or stale backend running silently.

## Triage: a deploy failed

**`make deploy` itself failed (non-zero exit).** Read the last thing it
printed — the recipe stops at the first failing step, so the message tells you
which stage failed:

- *Backup step failed* — Postgres came up but `pg_dump` errored. Check disk
  space (`df -h`) and `docker compose ... logs postgres`. The deploy did not
  proceed; nothing changed.
- *Build/compose step failed* — a Docker build error or a container that
  exited immediately. `docker compose -p <project> logs backend` /
  `... logs frontend` for the stack trace. Nothing was pruned or smoke-tested;
  the previous containers are still whatever they were before this attempt
  (compose leaves the prior container running until the new one is healthy
  enough to replace it, but an outright build failure means no new container
  was even created).
- *Post-deploy smoke failed* — containers came up but `/health` never reported
  the new SHA within ~30s. Most common causes: a migration panicked on boot
  (`docker compose ... logs backend` — look for a migration error before the
  HTTP server starts), the backend is crash-looping, or a config value is
  missing/wrong for this environment. This is the case to roll back from (see
  below) if triage doesn't turn up a quick fix.

**Auto-deploy timer fired and failed.** It notifies
(`infra/notify/notify.sh`) with the same detail and leaves the previous
build running — the timer is self-healing by design (see
[`infra/auto-deploy/README.md`](../../infra/auto-deploy/README.md) § Notes):
a failed build doesn't advance "what's deployed," so the next tick just
retries the same commit. If a bad commit landed on `main`, retrying won't
help; fix forward on `main` or roll back the *instance* to the last good SHA
(below) while `main` is fixed.

## Rollback: pin to a known-good commit

Compose builds from the working tree, so "run an older build" is "check out an
older commit, then deploy" — `command make deploy` supports this directly via
`GIT_REF`:

```sh
command make deploy GIT_REF=<sha-or-tag> [FLAVOR=prod]
```

This:

- Fetches `origin` and verifies `GIT_REF` resolves to a known commit (fails
  loudly if it's an unfetched remote-only ref — run `git fetch origin` by hand
  if needed).
- Refuses if the deploy checkout has uncommitted changes (a rollback should
  never silently fold in local edits).
- Prints a reminder to pause the auto-deploy timer, then checks out `GIT_REF`
  and runs the normal deploy sequence above (backup → build → smoke) against
  that commit.

**Pause auto-deploy first if it's installed for this flavor**, or the timer's
next tick will see `origin/main` has moved past `GIT_REF` and fast-forward
right back:

```sh
systemctl --user disable --now offbook-deploy@<flavor>.timer   # dev or prod
```

Re-enable it once `main` is fixed and you're ready to track it again:

```sh
systemctl --user enable --now offbook-deploy@<flavor>.timer
```

**Returning to latest** after the incident is resolved: check out `main` again
and deploy normally.

```sh
git checkout main && git pull --ff-only && command make deploy [FLAVOR=prod]
```

### Rollback vs. restore

Rolling back (`GIT_REF=`) only changes **which code** is running — it never
touches the database. That's exactly what you want when the code is broken but
the schema is fine (the common case: a bad handler, a broken build, a bug in
business logic).

It is **not** enough when the failed deploy ran a migration that the older
code can't work against — an older binary querying a column the new migration
dropped will itself error. In that case:

1. Roll back the code (`GIT_REF=` to the commit *before* the migration).
2. If the migration already changed data in a way the old code can't tolerate,
   restore the pre-migration backup instead — every deploy takes one
   automatically right before migrating (step 2 above). See
   [`docs/ops/backup-restore.md`](backup-restore.md) § Restoring.

This is the same expand→migrate→contract discipline from
[AGENTS.md § Migration Safety](../../AGENTS.md): a migration is designed to be
safe for the *currently deployed* code, so a same-stage rollback (code only,
no schema change in flight) should never need a restore. Reach for restore
only when a contract-stage migration already dropped something the rolled-back
code still reads.

## Manual smoke check

To re-run just the post-deploy check (e.g. after fixing something by hand
without a full redeploy):

```sh
OFFBOOK_COMPOSE="docker compose --env-file .env -p offbook -f docker-compose.yml -f docker-compose.tailscale.yml" \
  OFFBOOK_PROJECT=offbook \
  infra/deploy/post-deploy-smoke.sh "$(git rev-parse --short HEAD)"
```

(Match `OFFBOOK_COMPOSE`/`OFFBOOK_PROJECT` to the flavor — swap in the prod
compose files/project for a prod check. Easiest in practice: just rerun
`command make deploy [FLAVOR=prod]`, which is idempotent.)
