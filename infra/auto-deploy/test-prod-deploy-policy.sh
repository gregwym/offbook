#!/usr/bin/env bash
# Test policy before any Git/Docker operations; safe to run without credentials.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET="$ROOT/infra/auto-deploy/auto-deploy.sh"
OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT
assert_refusal() {
  local rc=0
  "$@" >"$OUT" 2>&1 || rc=$?
  if [[ "$rc" -ne 2 ]] || ! grep -q 'refusing production auto-deploy' "$OUT"; then
    echo "prod guard expected exit 2 and refusal message; got $rc" >&2
    cat "$OUT" >&2
    exit 1
  fi
}
assert_refusal env -u OFFBOOK_DEPLOY_BRANCH OFFBOOK_FLAVOR=prod bash "$TARGET"
assert_refusal env OFFBOOK_FLAVOR=prod OFFBOOK_DEPLOY_BRANCH=main bash "$TARGET"
echo "Production auto-deploy guard checks passed"
