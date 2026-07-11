#!/usr/bin/env bash
# Prove the latest backup actually restores — an unrestored backup is not a
# backup. Restores the newest dump into a THROWAWAY scratch database (never the
# live one), sanity-checks it, then drops the scratch DB. Safe to run on a
# schedule against a live instance: it only reads the live DB's name/user and
# writes to a scratch DB it owns.
#
#   verify.sh [backup_file]   # default: newest dump in BACKUP_DIR
#
# Sanity checks after restore:
#   • schema_migrations has a row (schema present) and is not dirty
#   • the 20 seeded system categories are present (seed/data integrity)
#
# Env: OFFBOOK_COMPOSE (required), OFFBOOK_PROJECT, BACKUP_DIR (to find latest).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=infra/backup/lib.sh
. "$HERE/lib.sh"
_init_compose

PROJECT="${OFFBOOK_PROJECT:?OFFBOOK_PROJECT required}"
BACKUP_DIR="${BACKUP_DIR:-backups/$PROJECT}"

file="${1:-}"
if [ -z "$file" ]; then
	file="$(ls -1 "$BACKUP_DIR/${PROJECT}"-*.dump 2>/dev/null | sort | tail -1 || true)"
fi
[ -n "$file" ] && [ -f "$file" ] || { echo "error: no backup to verify (looked in $BACKUP_DIR)." >&2; exit 2; }

ensure_postgres_up

live_db="$(db_name)"
scratch="${live_db}_restore_check"
echo "Verifying $file by restoring into scratch DB '$scratch'…"

cleanup() {
	pg_sh "psql -U \"\$POSTGRES_USER\" -d postgres -v ON_ERROR_STOP=1 \
	  -c \"DROP DATABASE IF EXISTS \\\"$scratch\\\";\"" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Fresh scratch DB.
pg_sh "psql -U \"\$POSTGRES_USER\" -d postgres -v ON_ERROR_STOP=1 \
  -c \"DROP DATABASE IF EXISTS \\\"$scratch\\\";\" \
  -c \"CREATE DATABASE \\\"$scratch\\\" OWNER \\\"\$POSTGRES_USER\\\";\""

# Restore into it.
if ! "${COMPOSE[@]}" exec -T postgres sh -c "pg_restore -U \"\$POSTGRES_USER\" -d \"$scratch\" --no-owner --exit-on-error" < "$file"; then
	echo "VERIFY FAILED: pg_restore could not restore $file" >&2
	exit 1
fi

# Check 1: schema present (a migration version row exists).
ver="$(pg_sh "psql -U \"\$POSTGRES_USER\" -d \"$scratch\" -tAc 'SELECT version FROM schema_migrations LIMIT 1;'" | tr -d '[:space:]')"
if [ -z "$ver" ]; then
	echo "VERIFY FAILED: restored DB has no schema_migrations version" >&2
	exit 1
fi

# Check 2: not left dirty.
dirty="$(pg_sh "psql -U \"\$POSTGRES_USER\" -d \"$scratch\" -tAc 'SELECT dirty FROM schema_migrations LIMIT 1;'" | tr -d '[:space:]')"
if [ "$dirty" = "t" ]; then
	echo "VERIFY FAILED: restored schema_migrations is dirty at version $ver" >&2
	exit 1
fi

# Check 3: seed/data integrity — the 20 system categories survived the round-trip.
cats="$(pg_sh "psql -U \"\$POSTGRES_USER\" -d \"$scratch\" -tAc 'SELECT count(*) FROM categories WHERE is_system;'" | tr -d '[:space:]')"
if [ -z "$cats" ] || [ "$cats" -lt 20 ]; then
	echo "VERIFY FAILED: expected >=20 system categories in restored DB, found '${cats:-0}'" >&2
	exit 1
fi

echo "verify OK — $file restores cleanly (schema version $ver, $cats system categories)"
