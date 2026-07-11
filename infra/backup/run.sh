#!/usr/bin/env bash
# Nightly backup entrypoint for one FLAVOR: take a backup, then verify the
# latest dump restores. This is what the offbook-backup@<flavor>.timer fires;
# it calls back into `make` so it always uses the same COMPOSE/config plumbing
# as an interactive `command make backup` (mirrors infra/auto-deploy/auto-deploy.sh).
#
# Env overrides:
#   OFFBOOK_REPO      repo checkout path (default: this script's repo root)
#   OFFBOOK_FLAVOR    flavor -> make …  (default: dev)
#
# Failure handling: on any failure we call notify_failure, which logs and
# delivers via infra/notify/notify.sh (the #360 ntfy/webhook seam — reads
# NOTIFY_NTFY_URL / NOTIFY_WEBHOOK_URL from the environment) by default.
# BACKUP_NOTIFY_CMD, if set, overrides that default with an arbitrary command
# that receives the message as $1. Exiting non-zero also lets systemd's
# OnFailure= (or the journal) surface the problem independently.
set -euo pipefail

REPO="${OFFBOOK_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"
FLAVOR="${OFFBOOK_FLAVOR:-dev}"

# Single-flight per flavor: a slow verify shouldn't overlap the next timer tick.
exec 9>"${TMPDIR:-/tmp}/offbook-backup-${FLAVOR}.lock"
if ! flock -n 9; then
	echo "backup[$FLAVOR]: a run is already in progress, skipping"
	exit 0
fi

cd "$REPO"

notify_failure() {
	local msg="offbook backup[$FLAVOR] FAILED: $1"
	echo "$msg" >&2
	if [ -n "${BACKUP_NOTIFY_CMD:-}" ]; then
		# Explicit override: BACKUP_NOTIFY_CMD receives the message as $1.
		sh -c "$BACKUP_NOTIFY_CMD" _ "$msg" || echo "backup[$FLAVOR]: notifier command failed" >&2
	else
		# Default (#360): deliver via the shared ntfy/webhook helper. A no-op,
		# exit 0, when neither NOTIFY_NTFY_URL nor NOTIFY_WEBHOOK_URL is set.
		"$REPO/infra/notify/notify.sh" "offbook backup[$FLAVOR] failed" "$msg" \
			|| echo "backup[$FLAVOR]: notifier command failed" >&2
	fi
}

if ! make backup FLAVOR="$FLAVOR"; then
	notify_failure "pg_dump/backup step failed"
	exit 1
fi

if ! make backup-verify FLAVOR="$FLAVOR"; then
	notify_failure "restore-verification failed (the latest dump did not restore)"
	exit 1
fi

echo "backup[$FLAVOR]: backup + verify OK"
