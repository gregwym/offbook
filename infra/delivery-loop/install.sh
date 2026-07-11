#!/bin/bash
# Install the Offbook autonomous-delivery LaunchAgent (macOS, user-level).
#
#   command make delivery-install                # from the repo root
#   OFFBOOK_DELIVERY_INTERVAL=21600 install.sh   # custom cadence (seconds)
#
# What it does:
#   1. Creates/refreshes a DEDICATED delivery clone (default
#      ~/src/offbook-delivery) so the loop never touches your working checkout.
#   2. Renders com.offbook.delivery.plist and bootstraps it into launchd.
# Idempotent — re-running updates the clone, plist, and schedule in place.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DELIVERY_DIR="${OFFBOOK_DELIVERY_DIR:-$HOME/src/offbook-delivery}"
INTERVAL="${OFFBOOK_DELIVERY_INTERVAL:-10800}"   # 3h default
LOG_DIR="$HOME/Library/Logs/offbook-delivery"
PLIST_DST="$HOME/Library/LaunchAgents/com.offbook.delivery.plist"
LABEL="com.offbook.delivery"

echo "== Offbook delivery loop install =="

# ── Preflight ────────────────────────────────────────────────────────────────
for bin in claude git gh; do
  command -v "$bin" >/dev/null || { echo "FATAL: '$bin' not on PATH" >&2; exit 1; }
done
gh auth status >/dev/null 2>&1 || { echo "FATAL: gh is not authenticated (run 'gh auth login')" >&2; exit 1; }

# ── Delivery clone ───────────────────────────────────────────────────────────
ORIGIN_URL="$(git -C "$REPO_ROOT" remote get-url origin)"
if [ ! -d "$DELIVERY_DIR/.git" ]; then
  echo "Cloning $ORIGIN_URL -> $DELIVERY_DIR"
  git clone "$ORIGIN_URL" "$DELIVERY_DIR"
else
  echo "Refreshing existing delivery clone at $DELIVERY_DIR"
  git -C "$DELIVERY_DIR" fetch origin
  git -C "$DELIVERY_DIR" checkout -q main
  git -C "$DELIVERY_DIR" pull --ff-only -q
fi

# ── Render + load the LaunchAgent ────────────────────────────────────────────
mkdir -p "$LOG_DIR" "$HOME/Library/LaunchAgents"
RUNNER="$DELIVERY_DIR/infra/delivery-loop/run-iteration.sh"
chmod +x "$RUNNER" "$SCRIPT_DIR/run-iteration.sh"

sed -e "s|__RUNNER__|$RUNNER|g" \
    -e "s|__DELIVERY_DIR__|$DELIVERY_DIR|g" \
    -e "s|__LOG_DIR__|$LOG_DIR|g" \
    -e "s|__INTERVAL__|$INTERVAL|g" \
    "$SCRIPT_DIR/com.offbook.delivery.plist.template" > "$PLIST_DST"

UID_NUM="$(id -u)"
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST_DST"

# Claude Code ignores the repo's permissions.allow list in an untrusted
# workspace, which starves the auto-mode iteration of its pre-approvals.
# Trust is the owner's to grant — detect and instruct, never self-modify.
if command -v jq >/dev/null && [ -f "$HOME/.claude.json" ]; then
  trusted="$(jq -r --arg d "$DELIVERY_DIR" '.projects[$d].hasTrustDialogAccepted // false' "$HOME/.claude.json")"
  if [ "$trusted" != "true" ]; then
    echo
    echo "⚠️  $DELIVERY_DIR is not a trusted Claude workspace — the repo allowlist"
    echo "   will be ignored. Trust it once (owner action):"
    echo "     cd $DELIVERY_DIR && claude   # accept the trust dialog, then exit"
  fi
fi

echo
echo "Installed. The loop fires every $((INTERVAL / 60)) minutes."
echo "  runner:   $RUNNER"
echo "  logs:     $LOG_DIR/iteration-*.log"
echo "  run now:  launchctl kickstart gui/$UID_NUM/$LABEL"
echo "  pause:    launchctl bootout gui/$UID_NUM/$LABEL"
echo "  remove:   command make delivery-uninstall"
