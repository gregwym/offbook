#!/usr/bin/env bash
# Poll origin and redeploy the local offbook-dev stack when the branch moves.
#
# Pull-based by design: it only ever fetches and builds the deploy branch
# (default `main`), never PR/fork code, and needs no inbound webhook — so it
# works behind NAT/Tailscale and adds no attack surface. Intended to run on a
# timer on the deploy host (see offbook-deploy-dev.{service,timer}).
#
# "What's running" is read from the build SHA in GET /health (#310), not from
# the checkout, so a failed build is retried on the next tick (the running
# version stays behind until a build actually succeeds).
#
# Reuses `make deploy-dev` for the actual build + recreate.
#
# Env overrides:
#   OFFBOOK_REPO           repo checkout path        (default: $HOME/offbook)
#   OFFBOOK_DEPLOY_BRANCH  branch to track           (default: main)
#   OFFBOOK_HEALTH_URL     local health endpoint     (default: http://localhost:8000/api/v1/health)
set -euo pipefail

REPO="${OFFBOOK_REPO:-$HOME/offbook}"
BRANCH="${OFFBOOK_DEPLOY_BRANCH:-main}"
HEALTH_URL="${OFFBOOK_HEALTH_URL:-http://localhost:8000/api/v1/health}"

# Single-flight: a build can outlast the timer interval, so skip overlapping
# runs rather than stack them up.
exec 9>"${TMPDIR:-/tmp}/offbook-auto-deploy-dev.lock"
if ! flock -n 9; then
	echo "auto-deploy: a run is already in progress, skipping"
	exit 0
fi

cd "$REPO"

git fetch --quiet origin "$BRANCH"
remote="$(git rev-parse --short "origin/$BRANCH")"

# Ground truth for "what's deployed" is the running build's reported SHA.
# Empty (backend down / unreachable) is treated as "needs deploy" so a crashed
# stack self-heals on the next tick.
deployed="$(curl -fsS "$HEALTH_URL" 2>/dev/null | grep -oE '"version":"[^"]+"' | cut -d'"' -f4 || true)"

if [ -n "$deployed" ] && [ "$deployed" = "$remote" ]; then
	exit 0 # already running origin/$BRANCH
fi

echo "auto-deploy: deployed=${deployed:-none} -> origin/$BRANCH=$remote; redeploying"
git checkout --quiet "$BRANCH"
git merge --ff-only --quiet "origin/$BRANCH"
# Env-agnostic: deploys the instance configured by ENV_FILE (default .env).
# Override OFFBOOK_ENV_FILE to point a prod host's timer at .env.prod.
make deploy ENV_FILE="${OFFBOOK_ENV_FILE:-.env}"
