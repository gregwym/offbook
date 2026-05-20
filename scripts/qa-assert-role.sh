#!/usr/bin/env sh
set -eu

base="$(basename "$PWD")"
if [ "${OFFBOOK_ROLE:-}" = "qa" ] || [ "$base" = "offbook-qa" ] || [ "${base%*-qa}" != "$base" ]; then
  exit 0
fi

cat >&2 <<'EOF'
QA tooling requires a structural QA signal.

Run from a QA worktree such as ../offbook-qa, or set:

  export OFFBOOK_ROLE=qa
EOF
exit 2
