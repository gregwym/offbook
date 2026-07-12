#!/usr/bin/env bash
# Post-deploy smoke check (#361): after `make deploy` brings up/updates
# containers, poll /health inside the backend container and assert it
# reports status=ok AND the SHA that was just deployed. A container exec
# (not a host curl) so it works whether or not host ports are published —
# same trick as `make deployed-sha` (#310), which prod relies on since it
# binds none.
#
# A deploy that "succeeds" at `docker compose up` but leaves the backend
# crash-looping or serving a stale binary is exactly the silent failure this
# guards against. Non-zero exit + a notifier event on failure so a bad deploy
# announces itself instead of quietly serving errors.
#
# Usage: post-deploy-smoke.sh <expected-sha>
# Env:
#   OFFBOOK_COMPOSE   compose invocation (word-split), required — set by the
#                     Makefile the same way infra/backup/*.sh consume it.
#   OFFBOOK_PROJECT   project name, for messages (required)
#   SMOKE_RETRIES     poll attempts (default 10)
#   SMOKE_DELAY       seconds between attempts (default 3)
set -uo pipefail

expected_sha="${1:?usage: post-deploy-smoke.sh <expected-sha>}"
retries="${SMOKE_RETRIES:-10}"
delay="${SMOKE_DELAY:-3}"

# shellcheck disable=SC2206
compose=(${OFFBOOK_COMPOSE:?OFFBOOK_COMPOSE not set})
project="${OFFBOOK_PROJECT:?OFFBOOK_PROJECT not set}"

status=""
version=""
attempt=1
while [ "$attempt" -le "$retries" ]; do
	body="$("${compose[@]}" exec -T backend wget -qO- http://localhost:8000/api/v1/health 2>/dev/null)"
	status="$(printf '%s' "$body" | grep -oE '"status":"[^"]+"' | cut -d'"' -f4)"
	version="$(printf '%s' "$body" | grep -oE '"version":"[^"]+"' | cut -d'"' -f4)"

	if [ "$status" = "ok" ] && [ "$version" = "$expected_sha" ]; then
		echo "post-deploy smoke[$project]: OK (status=ok version=$version)"
		exit 0
	fi

	attempt=$((attempt + 1))
	[ "$attempt" -le "$retries" ] && sleep "$delay"
done

msg="post-deploy smoke failed for $project: expected version=$expected_sha, got status='${status:-<none>}' version='${version:-<none>}' after $retries attempts (~$((retries * delay))s)"
echo "$msg" >&2

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
"$repo_root/infra/notify/notify.sh" "offbook deploy smoke failed ($project)" "$msg" \
	|| echo "post-deploy-smoke: notifier command failed" >&2

exit 1
