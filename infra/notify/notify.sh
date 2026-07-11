#!/usr/bin/env bash
# Shared notify helper for shell-driven infra (backup.sh, auto-deploy.sh).
# Mirrors internal/service/notify's ntfy + webhook seam so failures raised
# outside the Go process (cron/systemd paths) use the same config and
# delivery targets as in-app alerts.
#
# Usage: notify.sh "<subject>" "<detail>"
# Reads NOTIFY_NTFY_URL / NOTIFY_WEBHOOK_URL from the environment (the caller
# is expected to have already sourced .env / .env.prod). No-op, exit 0, if
# neither is set — this must never be the reason a backup/deploy script fails.
set -uo pipefail   # deliberately not -e: a failed notify must not mask/replace
                    # the original failure the caller is already handling

subject="${1:?usage: notify.sh <subject> <detail>}"
detail="${2:-}"

# json_escape prints a valid JSON string literal (including the surrounding
# quotes) for its argument. No external dependency — plain POSIX-ish shell so
# it works the same on a bare systemd host as it does in the backend
# container.
json_escape() {
	local s=$1
	s=${s//\\/\\\\}   # backslash first, so later substitutions don't double-escape
	s=${s//\"/\\\"}   # double quote
	s=${s//$'\n'/\\n} # newline
	s=${s//$'\r'/\\r} # carriage return
	s=${s//$'\t'/\\t} # tab
	printf '"%s"' "$s"
}

if [ -n "${NOTIFY_NTFY_URL:-}" ]; then
	curl -fsS -m 10 -X POST "$NOTIFY_NTFY_URL" \
		-H "Title: $subject" -H "Priority: high" \
		--data-raw "$detail" >/dev/null \
		|| echo "notify.sh: ntfy delivery failed" >&2
fi

if [ -n "${NOTIFY_WEBHOOK_URL:-}" ]; then
	payload="{\"subject\":$(json_escape "$subject"),\"detail\":$(json_escape "$detail"),\"time\":\"$(date -u +%FT%TZ)\"}"
	curl -fsS -m 10 -X POST "$NOTIFY_WEBHOOK_URL" \
		-H "Content-Type: application/json" -d "$payload" >/dev/null \
		|| echo "notify.sh: webhook delivery failed" >&2
fi

exit 0
