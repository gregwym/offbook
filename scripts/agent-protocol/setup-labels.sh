#!/usr/bin/env bash
# Safe by default. Explicit --apply is needed to write labels.
set -euo pipefail
MODE="${1:---dry-run}"
if [[ "$MODE" != "--dry-run" && "$MODE" != "--apply" ]]; then
  echo "usage: bash scripts/agent-protocol/setup-labels.sh [--dry-run|--apply]" >&2
  exit 2
fi
labels=(
  "agent:research|8b5cf6|Product research pending"
  "agent:ready|0e8a16|Product acceptance ready"
  "agent:claimed|0366d6|Canonical implementation branch claimed"
  "agent:blocked|b60205|Human decision or environment blocker"
  "agent:qa-needed|fbca04|Independent QA needed"
  "agent:qa-failed|d93f0b|Independent QA failed or blocked"
  "agent:qa-passed|0e8a16|Informational QA pass, not a trusted gate"
  "agent:qa-verdict|5319e7|Nudge to recalculate trusted QA verdict check"
)
for entry in "${labels[@]}"; do
  IFS='|' read -r name color description <<< "$entry"
  if [[ "$MODE" == "--dry-run" ]]; then
    printf 'would create/update label: %s (#%s) — %s\n' "$name" "$color" "$description"
  else
    command -v gh >/dev/null || { echo "gh CLI is required" >&2; exit 2; }
    gh label create "$name" --color "$color" --description "$description" --force
  fi
done
