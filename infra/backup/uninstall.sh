#!/usr/bin/env bash
# Remove the user-level nightly backup timer for one FLAVOR.
#
#   command make backup-uninstall              # dev
#   command make backup-uninstall FLAVOR=prod  # prod
set -euo pipefail

FLAVOR="${OFFBOOK_FLAVOR:-${FLAVOR:-dev}}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

if ! command -v systemctl >/dev/null 2>&1; then
	echo "error: systemctl not found." >&2
	exit 1
fi

systemctl --user disable --now "offbook-backup@${FLAVOR}.timer" 2>/dev/null || true

# Drop the shared template files only when no other flavor's timer is still
# enabled.
if ! systemctl --user list-unit-files 'offbook-backup@*.timer' 2>/dev/null | grep -q '\benabled\b'; then
	rm -f "$UNIT_DIR/offbook-backup@.service" "$UNIT_DIR/offbook-backup@.timer"
	echo "Removed shared offbook-backup@ unit files (no flavors remain enabled)."
fi

systemctl --user daemon-reload
echo "Uninstalled offbook-backup@${FLAVOR}.timer."
