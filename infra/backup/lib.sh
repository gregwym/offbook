#!/usr/bin/env bash
# Shared helpers for the Offbook backup/restore scripts. Sourced, not executed.
#
# The one piece of host-specific state every script needs is HOW to reach the
# Postgres container for this instance — i.e. the `docker compose … -p <project>
# -f …` invocation. The Makefile owns that (it varies by FLAVOR) and hands it in
# as the OFFBOOK_COMPOSE env string, which we word-split into the COMPOSE array.
# This keeps the scripts convention-agnostic and independently testable: point
# OFFBOOK_COMPOSE at any compose project with a `postgres` service and they work.

# Parse OFFBOOK_COMPOSE into the COMPOSE array used by every db call.
_init_compose() {
	if [ -z "${OFFBOOK_COMPOSE:-}" ]; then
		echo "error: OFFBOOK_COMPOSE is not set (run via 'command make backup' etc.)." >&2
		exit 2
	fi
	# Intentional word-split of a controlled, script-internal value.
	# shellcheck disable=SC2206
	COMPOSE=( ${OFFBOOK_COMPOSE} )
}

# pg_sh runs a shell snippet INSIDE the postgres container, so it always uses the
# container's own $POSTGRES_USER/$POSTGRES_DB and the matching client binaries —
# no host-side knowledge of the DB name or a local psql install required.
pg_sh() {
	"${COMPOSE[@]}" exec -T postgres sh -c "$1"
}

# db_name / db_user echo the live values from the container environment.
db_name() { pg_sh 'printf %s "$POSTGRES_DB"'; }
db_user() { pg_sh 'printf %s "$POSTGRES_USER"'; }

# ensure_postgres_up fails fast with a clear message if the stack isn't running.
ensure_postgres_up() {
	if ! "${COMPOSE[@]}" ps --status running postgres 2>/dev/null | grep -q postgres; then
		echo "error: the postgres service for this instance is not running. Start it with 'command make deploy' first." >&2
		exit 1
	fi
}
