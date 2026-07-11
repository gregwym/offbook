#!/usr/bin/env bash
# Restore an Offbook instance's database from a pg_dump archive. This is the
# recovery path for a lost postgres volume, a bad migration, or a fat-fingered
# teardown — the counterpart to backup.sh.
#
#   restore.sh <backup_file>
#
# DESTRUCTIVE: it drops and recreates the live database, then restores the dump
# into it. The Makefile front door (`command make restore BACKUP=…`) gates this
# with a typed-confirmation prompt (skip with FORCE=1). Stop the backend first
# in a real recovery so nothing races the restore; this script also terminates
# stray connections before the drop.
#
# Env: OFFBOOK_COMPOSE (required, provided by the Makefile).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=infra/backup/lib.sh
. "$HERE/lib.sh"
_init_compose

file="${1:?backup file required}"
[ -f "$file" ] || { echo "error: backup file '$file' not found." >&2; exit 2; }

ensure_postgres_up

db="$(db_name)"
echo "Restoring $file into database '$db'…"

# Drop + recreate the target DB from the maintenance 'postgres' DB. $POSTGRES_DB
# / $POSTGRES_USER expand inside the container; the \"…\" keep the identifiers
# quoted for psql. ON_ERROR_STOP makes any failure fatal.
pg_sh '
set -e
psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '"'"'$POSTGRES_DB'"'"' AND pid <> pg_backend_pid();" \
  -c "DROP DATABASE IF EXISTS \"$POSTGRES_DB\";" \
  -c "CREATE DATABASE \"$POSTGRES_DB\" OWNER \"$POSTGRES_USER\";"
'

# Stream the archive into pg_restore running inside the container. --no-owner so
# the restore doesn't fail on role mismatches; the single app role owns it.
if ! "${COMPOSE[@]}" exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --exit-on-error' < "$file"; then
	echo "error: pg_restore failed — the database was recreated but may be incomplete. Investigate before serving traffic." >&2
	exit 1
fi

echo "restore OK — '$db' restored from $file"
echo "Restart the backend so it reconnects cleanly:  command make deploy"
