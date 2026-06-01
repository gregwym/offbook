#!/usr/bin/env bash
# Remove the user-level auto-deploy timer for one FLAVOR.
#
#   command make auto-deploy-uninstall              # dev
#   command make auto-deploy-uninstall FLAVOR=prod  # prod
set -euo pipefail

FLAVOR="${OFFBOOK_FLAVOR:-${FLAVOR:-dev}}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

if ! command -v systemctl >/dev/null 2>&1; then
	echo "error: systemctl not found." >&2
	exit 1
fi

systemctl --user disable --now "offbook-deploy@${FLAVOR}.timer" 2>/dev/null || true

# Drop the shared template files only when no other flavor's timer is still
# enabled (the templates are harmless on their own, but tidy up when unused).
if ! systemctl --user list-unit-files 'offbook-deploy@*.timer' 2>/dev/null | grep -q '\benabled\b'; then
	rm -f "$UNIT_DIR/offbook-deploy@.service" "$UNIT_DIR/offbook-deploy@.timer"
	echo "Removed shared offbook-deploy@ unit files (no flavors remain enabled)."
fi

systemctl --user daemon-reload
echo "Uninstalled offbook-deploy@${FLAVOR}.timer."
