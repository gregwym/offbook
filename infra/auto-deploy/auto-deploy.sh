#!/usr/bin/env bash
# Poll origin and redeploy the local Offbook stack when the deploy branch moves.
#
# Pull-based by design: it only ever fetches and builds the deploy branch
# (default `main`), never PR/fork code, and needs no inbound webhook — so it
# works behind NAT/Tailscale and adds no attack surface. Intended to run on a
# user-level systemd timer (offbook-deploy@<flavor>.timer); see install.sh.
#
# "What's running" is read from the build SHA the backend reports at /health
# (#310) via `make deployed-sha` — a container exec, so it works whether or not
# host ports are published (prod binds none). A failed build is retried next
# tick (the running version stays behind until a build actually succeeds).
#
# Env overrides:
#   OFFBOOK_REPO           repo checkout path  (default: this script's repo root)
#   OFFBOOK_FLAVOR         deploy flavor       (default: dev) -> make deploy FLAVOR=…
#   OFFBOOK_DEPLOY_BRANCH  branch to track     (default: main)
set -euo pipefail

REPO="${OFFBOOK_REPO:-$(cd "$(dirname "$0")/../.." && pwd)}"
FLAVOR="${OFFBOOK_FLAVOR:-dev}"
BRANCH="${OFFBOOK_DEPLOY_BRANCH:-main}"

# Single-flight: a build can outlast the timer interval, so skip overlapping
# runs (per flavor) rather than stack them up.
exec 9>"${TMPDIR:-/tmp}/offbook-auto-deploy-${FLAVOR}.lock"
if ! flock -n 9; then
	echo "auto-deploy[$FLAVOR]: a run is already in progress, skipping"
	exit 0
fi

cd "$REPO"

git fetch --quiet origin "$BRANCH"
remote="$(git rev-parse --short "origin/$BRANCH")"

# Ground truth for "what's deployed" is the running build's reported SHA. Empty
# (backend down / unreachable) is treated as "needs deploy" so a crashed stack
# self-heals on the next tick.
deployed="$(make deployed-sha FLAVOR="$FLAVOR" 2>/dev/null | tail -1 || true)"

if [ -n "$deployed" ] && [ "$deployed" = "$remote" ]; then
	exit 0 # already running origin/$BRANCH
fi

echo "auto-deploy[$FLAVOR]: deployed=${deployed:-none} -> origin/$BRANCH=$remote; redeploying"
git checkout --quiet "$BRANCH"
git merge --ff-only --quiet "origin/$BRANCH"
make deploy FLAVOR="$FLAVOR"
