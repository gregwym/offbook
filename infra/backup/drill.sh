#!/usr/bin/env bash
# Lost-volume disaster-recovery drill (#605): proves the backup → restore path in
# docs/ops/backup-restore.md brings a user's money ledger back EXACTLY, not just
# a schema. Runs on every PR in CI and locally via `command make backup-drill`.
#
# On a throwaway compose project (offbook-drill, its own named volume and ports):
#   1. boot postgres + backend, seed synthetic data through the public API
#      (first-boot admin, two manual accounts, exact-decimal transactions incl.
#      sub-cent amounts, a manual category pick)
#   2. fingerprint it: API-visible balances + transactions, and SQL money sums
#      cast to text (so 0.000000000000000001 differences are caught)
#   3. backup.sh, then verify.sh                      (the nightly job)
#   4. LOSE THE VOLUME (down -v), recreate a fresh empty stack, assert empty
#   5. restore.sh, restart the backend                (runbook "lost volume")
#   6. sign in with the original password; the fingerprint must be identical
#
# Synthetic data only. Never points at a dev/QA/prod project, and tears the
# drill project down (volumes included) on exit.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

PROJECT=offbook-drill
API="http://localhost:28000/api/v1"
COMPOSE_STR="docker compose -p $PROJECT -f docker-compose.yml -f infra/backup/docker-compose.drill.yml"
# shellcheck disable=SC2206
COMPOSE=( $COMPOSE_STR )
WORK="$(mktemp -d)"
export DRILL_SESSION_SECRET="$(openssl rand -hex 32)"
export OFFBOOK_COMPOSE="$COMPOSE_STR" OFFBOOK_PROJECT="$PROJECT" BACKUP_DIR="$WORK/backups"
EMAIL="drill-admin@example.test"
PASSWORD="drill-$(openssl rand -hex 8)"
JAR="$WORK/cookies"

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "── drill FAILED; backend logs ──" >&2
		"${COMPOSE[@]}" logs --no-color --tail 80 backend >&2 || true
	fi
	"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$WORK"
	exit "$status"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "DRILL FAILED: $*" >&2; exit 1; }

up() {
	"${COMPOSE[@]}" up -d --build --wait postgres backend
	for _ in $(seq 1 60); do
		curl -sf "$API/health" >/dev/null && return 0
		sleep 2
	done
	fail "backend did not become healthy"
}

# api METHOD PATH [JSON] — cookie-authenticated call; fails the drill on non-2xx.
api() {
	local out code
	out="$WORK/resp"
	code="$(curl -sS -o "$out" -w '%{http_code}' -b "$JAR" -c "$JAR" -X "$1" \
		-H 'content-type: application/json' ${3:+--data "$3"} "$API$2")"
	case "$code" in 2*) cat "$out" ;; *) fail "$1 $2 → $code $(cat "$out")" ;; esac
}

sql() {
	"${COMPOSE[@]}" exec -T postgres sh -c "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -tAc \"$1\""
}

# fingerprint prints everything a user would notice changing, deterministically.
fingerprint() {
	echo "## accounts (API)"
	api GET /accounts | jq -S '[.data[] | {name, account_type, currency, balance}] | sort_by(.name)'
	echo "## transactions (API)"
	api GET '/transactions?limit=200' | jq -S \
		'[.data[] | {account_id, amount, description, category_id, transaction_date}] | sort_by(.description)'
	echo "## ledger (SQL)"
	sql "SELECT count(*), COALESCE(sum(amount), 0)::text FROM transactions WHERE deleted_at IS NULL;"
	sql "SELECT count(*) FROM accounts WHERE deleted_at IS NULL;"
	sql "SELECT count(*) FROM users;"
}

step "boot a fresh drill stack ($PROJECT)"
"${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
up

step "seed synthetic data through the API"
api POST /setup/admin "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"signup_mode\":\"invite_only\"}" >/dev/null
checking="$(api POST /accounts '{"name":"Drill Checking","institution_slug":"manual","account_type":"checking","currency":"USD","balance":"2500.00"}' | jq -r .data.id)"
card="$(api POST /accounts '{"name":"Drill Card","institution_slug":"manual","account_type":"credit_card","currency":"USD"}' | jq -r .data.id)"
category="$(api GET /categories | jq -r '[.data[] | select(.parent_id == null)][0].id')"
[ -n "$category" ] && [ "$category" != null ] || fail "no category to pick"
day="$(date -u +%Y-%m-01)"
tx() {
	api POST /transactions "{\"account_id\":$1,\"amount\":\"$2\",\"description\":\"$3\",\"transaction_date\":\"$day\",\"source\":\"manual\"${4:+,\"category_id\":$4}}" >/dev/null
}
tx "$checking" "1834.27" "Drill Payroll"
tx "$checking" "-62.19" "Drill Grocer" "$category"
tx "$checking" "-0.000000000000000001" "Drill Dust"
tx "$card" "-149.99" "Drill Hardware"
tx "$card" "12.50" "Drill Refund" "$category"

before="$WORK/before.txt"
fingerprint > "$before"
cat "$before"
grep -q '"Drill Dust"' "$before" || fail "seed did not land"

step "nightly backup + restore-verification"
infra/backup/backup.sh
infra/backup/verify.sh
dump="$(ls -1 "$BACKUP_DIR/$PROJECT"-*.dump | tail -1)"

step "lose the postgres volume, recreate an empty stack (runbook: lost volume, step 1)"
"${COMPOSE[@]}" down -v
up
[ "$(sql 'SELECT count(*) FROM users;' | tr -d '[:space:]')" = 0 ] || fail "fresh stack is not empty — the volume was not lost"

step "restore last night's dump (runbook steps 3–4)"
infra/backup/restore.sh "$dump"
"${COMPOSE[@]}" restart backend
up

step "sign in with the original password and compare"
rm -f "$JAR"
api POST /auth/signin "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" >/dev/null
after="$WORK/after.txt"
fingerprint > "$after"
if ! diff -u "$before" "$after"; then
	fail "restored ledger differs from the pre-loss ledger (diff above)"
fi
api GET /health | jq -e '.data.status == "ok"' >/dev/null || fail "health is not ok after restore"

echo
echo "drill OK — lost volume recovered from $(basename "$dump"); ledger identical"
