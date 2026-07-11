#!/usr/bin/env bash
# Optional off-host copy of a freshly-written backup. OPT-IN and no-op by
# default: an on-host backup still protects against a lost postgres volume or a
# bad migration, but not against total host loss. Copying off-host closes that
# gap — and needs a remote target + credentials that only the instance owner can
# supply, so this script never fabricates one.
#
#   offhost.sh <backup_file>
#
# Configure by setting, in the instance env file (.env / .env.prod):
#   BACKUP_REMOTE=rclone:myremote:offbook-backups   # rclone remote+path
#   BACKUP_REMOTE=restic:/srv/restic-repo           # restic repository
# rclone reads its own config/credentials; restic reads RESTIC_PASSWORD etc.
# When BACKUP_REMOTE is unset this exits 0 after a one-line note.
set -euo pipefail

file="${1:?backup file required}"
remote="${BACKUP_REMOTE:-}"

if [ -z "$remote" ]; then
	echo "off-host: BACKUP_REMOTE unset — keeping backups on-host only (see docs/ops/backup-restore.md)."
	exit 0
fi

kind="${remote%%:*}"
target="${remote#*:}"

case "$kind" in
rclone)
	command -v rclone >/dev/null 2>&1 || { echo "off-host: BACKUP_REMOTE requests rclone but it isn't installed." >&2; exit 1; }
	echo "off-host: rclone copy $file → $target"
	rclone copy "$file" "$target"
	;;
restic)
	command -v restic >/dev/null 2>&1 || { echo "off-host: BACKUP_REMOTE requests restic but it isn't installed." >&2; exit 1; }
	echo "off-host: restic backup $file → repo $target"
	restic -r "$target" backup "$file"
	;;
*)
	echo "off-host: unknown BACKUP_REMOTE scheme '$kind' (expected rclone:… or restic:…)." >&2
	exit 1
	;;
esac
echo "off-host: copy OK"
