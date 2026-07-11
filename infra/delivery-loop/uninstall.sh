#!/bin/bash
# Remove the Offbook autonomous-delivery LaunchAgent.
# Keeps the delivery clone and logs — delete those by hand if wanted:
#   rm -rf ~/src/offbook-delivery ~/Library/Logs/offbook-delivery
set -euo pipefail

LABEL="com.offbook.delivery"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$PLIST"
echo "Removed $LABEL. Delivery clone and logs left in place."
