#!/bin/bash
# Consolidated PreToolUse dispatcher for Bash tool calls.
# Reads the hook payload from stdin, inspects .tool_input.command, and
# routes to the relevant check. Each check exits 0 (allow) by default
# when not applicable, so unrelated Bash commands pass through untouched.

set -e

INPUT=$(cat)
CMD=$(echo "$INPUT" | jq -r '.tool_input.command // ""')

# --- Check 1: branch name validation on `git checkout -b ...` ---
if [[ "$CMD" =~ ^[[:space:]]*git[[:space:]]+checkout[[:space:]]+-b[[:space:]]+ ]]; then
  BRANCH=$(echo "$CMD" | sed -E 's/^[[:space:]]*git[[:space:]]+checkout[[:space:]]+-b[[:space:]]+//' | awk '{print $1}')
  if ! [[ $BRANCH =~ ^(feature|fix|chore|docs)/[0-9]+-[a-z0-9-]+$ ]]; then
    echo "Branch must follow: {type}/{issue-number}-{slug}" >&2
    echo "Examples: feature/12-plaid-link, fix/34-decimal-rounding" >&2
    exit 2
  fi
fi

# --- Check 2: go vet + gofmt before `git commit ...` ---
# Skip silently if backend/ doesn't exist yet (pre-M1).
# gofmt is cheap and catches the most common CI lint failure (unformatted
# files); vet catches type/compile errors. Tests stay out of the hook to
# keep commit latency reasonable — run `make verify` before opening a PR.
if [[ "$CMD" =~ ^[[:space:]]*git[[:space:]]+commit ]]; then
  if [ -d backend ] && [ -f backend/go.mod ]; then
    UNFORMATTED=$(cd backend && gofmt -l . 2>/dev/null)
    if [ -n "$UNFORMATTED" ]; then
      echo "gofmt found unformatted files:" >&2
      echo "$UNFORMATTED" >&2
      echo "Fix with: cd backend && gofmt -w ." >&2
      exit 2
    fi
    if ! (cd backend && go vet ./...) 2>&1; then
      exit 2
    fi
  fi
fi

exit 0
