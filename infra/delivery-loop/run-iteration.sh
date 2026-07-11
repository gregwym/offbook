#!/bin/bash
# One firing of the Offbook autonomous delivery loop.
#
# Invoked by the com.offbook.delivery LaunchAgent (see install.sh) on a
# StartInterval cadence. Runs headless `claude -p` in auto mode (classifier-
# gated, no interactive prompts — NOT skip-permissions) in a DEDICATED delivery
# clone — never the owner's working checkout — so an active dev session and
# the loop can't fight over branches or the index.
#
# Design (mirrors docs/dev/autonomous-delivery.md § Durability):
#   * one iteration = at most one shipped PR, then exit
#   * a quota-capped firing fails cheaply; the next firing retries
#   * overlap-safe: launchd never double-starts a label, plus a stale-aware lock
set -u

LABEL="offbook-delivery"
DELIVERY_DIR="${OFFBOOK_DELIVERY_DIR:-$HOME/src/offbook-delivery}"
LOG_DIR="${OFFBOOK_DELIVERY_LOG_DIR:-$HOME/Library/Logs/offbook-delivery}"
LOCK_DIR="${TMPDIR:-/tmp}/${LABEL}.lock"
# Kill a runaway iteration after this many seconds (default 3h — one interval).
MAX_SECONDS="${OFFBOOK_DELIVERY_MAX_SECONDS:-10800}"
MODEL="${OFFBOOK_DELIVERY_MODEL:-claude-sonnet-5}"
PROMPT_FILE="$DELIVERY_DIR/infra/delivery-loop/iteration-prompt.md"

mkdir -p "$LOG_DIR"
LOG_FILE="$LOG_DIR/iteration-$(date +%Y%m%d-%H%M%S).log"
exec >>"$LOG_FILE" 2>&1

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

# ── Overlap guard ────────────────────────────────────────────────────────────
# launchd already refuses to double-start the label; the lock additionally
# protects against a manual run racing a scheduled one. Steal locks older
# than MAX_SECONDS — their owner is dead or about to be killed anyway.
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  age=$(( $(date +%s) - $(stat -f %m "$LOCK_DIR" 2>/dev/null || echo 0) ))
  if [ "$age" -lt "$MAX_SECONDS" ]; then
    log "another iteration holds the lock (age ${age}s) — skipping this firing"
    exit 0
  fi
  log "stealing stale lock (age ${age}s)"
  rm -rf "$LOCK_DIR"
  mkdir "$LOCK_DIR" || exit 1
fi
trap 'rm -rf "$LOCK_DIR"' EXIT

# ── Preflight ────────────────────────────────────────────────────────────────
for bin in claude git gh; do
  command -v "$bin" >/dev/null || { log "FATAL: '$bin' not on PATH"; exit 1; }
done
[ -d "$DELIVERY_DIR/.git" ] || { log "FATAL: delivery clone missing at $DELIVERY_DIR — run install.sh"; exit 1; }

cd "$DELIVERY_DIR" || exit 1
log "refreshing delivery clone at $DELIVERY_DIR"
git fetch origin || { log "FATAL: git fetch failed"; exit 1; }
git checkout -q main || { log "FATAL: cannot checkout main"; exit 1; }
git pull --ff-only -q || { log "FATAL: ff-only pull failed"; exit 1; }
[ -f "$PROMPT_FILE" ] || { log "FATAL: prompt file missing at $PROMPT_FILE"; exit 1; }

# ── Run one iteration, with a wall-clock watchdog ────────────────────────────
log "starting iteration: model=$MODEL max=${MAX_SECONDS}s"
claude -p "$(cat "$PROMPT_FILE")" \
  --model "$MODEL" \
  --permission-mode auto \
  --output-format text &
CLAUDE_PID=$!
( sleep "$MAX_SECONDS"; kill "$CLAUDE_PID" 2>/dev/null && echo "WATCHDOG: killed iteration after ${MAX_SECONDS}s" ) &
WATCHDOG_PID=$!
wait "$CLAUDE_PID"
STATUS=$?
kill "$WATCHDOG_PID" 2>/dev/null
wait "$WATCHDOG_PID" 2>/dev/null

log "iteration finished with exit status $STATUS"

# Keep two weeks of logs.
find "$LOG_DIR" -name 'iteration-*.log' -mtime +14 -delete 2>/dev/null

exit "$STATUS"
