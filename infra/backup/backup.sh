#!/usr/bin/env bash
# Dump one Offbook instance's database to a compressed, restorable archive,
# then apply retention and (optionally) copy off-host. This is the primitive
# `command make backup` runs; the nightly timer runs it via run.sh.
#
# The dump is taken inside the postgres container with pg_dump -Fc (custom
# format — compressed, restorable by pg_restore, forward-compatible across minor
# server versions). It is written to BACKUP_DIR on the HOST, deliberately OUTSIDE
# the postgres data volume, so losing that volume never loses the backups too.
#
# Env (all provided by the Makefile, with defaults):
#   OFFBOOK_COMPOSE     compose invocation for this instance (required)
#   OFFBOOK_PROJECT     compose project name, used in the filename (required)
#   BACKUP_DIR          where dumps land (default: backups/<project>)
#   BACKUP_KEEP_DAILY   most-recent dumps kept unconditionally (default: 7)
#   BACKUP_KEEP_WEEKLY  additional ISO-weeks kept, one dump each (default: 4)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=infra/backup/lib.sh
. "$HERE/lib.sh"
_init_compose

PROJECT="${OFFBOOK_PROJECT:?OFFBOOK_PROJECT required}"
BACKUP_DIR="${BACKUP_DIR:-backups/$PROJECT}"
KEEP_DAILY="${BACKUP_KEEP_DAILY:-7}"
KEEP_WEEKLY="${BACKUP_KEEP_WEEKLY:-4}"

ensure_postgres_up
mkdir -p "$BACKUP_DIR"

ts="$(date +%Y%m%d-%H%M%S)"
out="$BACKUP_DIR/${PROJECT}-${ts}.dump"
tmp="$out.partial"

echo "Backing up '$(db_name)' (project $PROJECT) → $out"
# Write to a .partial first so an interrupted dump never masquerades as a good
# backup (retention and restore only ever see complete .dump files).
if ! pg_sh 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' > "$tmp"; then
	rm -f "$tmp"
	echo "error: pg_dump failed" >&2
	"$HERE/../notify/notify.sh" "offbook backup failed ($PROJECT)" "pg_dump failed or produced an empty file"
	exit 1
fi
if [ ! -s "$tmp" ]; then
	rm -f "$tmp"
	echo "error: pg_dump produced an empty file" >&2
	"$HERE/../notify/notify.sh" "offbook backup failed ($PROJECT)" "pg_dump failed or produced an empty file"
	exit 1
fi
mv "$tmp" "$out"
echo "Wrote $(du -h "$out" | cut -f1) → $out"

"$HERE/prune.sh" "$BACKUP_DIR" "$PROJECT" "$KEEP_DAILY" "$KEEP_WEEKLY"

# Off-host copy is an opt-in seam (owner supplies the remote + credentials).
# No-op with a note when unconfigured — never invents a target.
"$HERE/offhost.sh" "$out" || {
	echo "warning: off-host copy step reported a failure (backup itself is intact)." >&2
}

echo "backup OK"
