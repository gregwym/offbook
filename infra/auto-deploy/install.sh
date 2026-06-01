#!/usr/bin/env bash
# Install the user-level systemd poll-and-redeploy timer for one FLAVOR.
# No sudo, no editing checked-in files. Re-runnable (idempotent).
#
#   command make auto-deploy-install              # dev
#   command make auto-deploy-install FLAVOR=prod  # prod
set -euo pipefail

FLAVOR="${OFFBOOK_FLAVOR:-${FLAVOR:-dev}}"
REPO="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

if ! command -v systemctl >/dev/null 2>&1; then
	echo "error: systemctl not found. The auto-deploy timer needs systemd (Linux host)." >&2
	exit 1
fi

mkdir -p "$UNIT_DIR"

# Render the templated service with THIS checkout's absolute path, copy the
# timer verbatim. Shared by every flavor (the instance name carries the flavor).
sed "s|__OFFBOOK_REPO__|$REPO|g" \
	"$REPO/infra/auto-deploy/offbook-deploy@.service" \
	> "$UNIT_DIR/offbook-deploy@.service"
cp "$REPO/infra/auto-deploy/offbook-deploy@.timer" "$UNIT_DIR/offbook-deploy@.timer"

systemctl --user daemon-reload
systemctl --user enable --now "offbook-deploy@${FLAVOR}.timer"

# Keep the timer firing when no user session is active (headless Pi/server).
# enable-linger usually needs root; fall back to a hint if it's not permitted.
if command -v loginctl >/dev/null 2>&1; then
	if ! loginctl enable-linger "$USER" 2>/dev/null; then
		echo "note: could not enable lingering automatically — the timer will only run while you're logged in." >&2
		echo "      For a headless host, run once:  sudo loginctl enable-linger $USER" >&2
	fi
fi

echo "Installed offbook-deploy@${FLAVOR}.timer (user service, repo: $REPO)."
echo "  Watch:        journalctl --user -u offbook-deploy@${FLAVOR} -f"
echo "  Run now:      systemctl --user start offbook-deploy@${FLAVOR}.service"
echo "  Next firing:  systemctl --user list-timers offbook-deploy@${FLAVOR}.timer"
