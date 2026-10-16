#!/usr/bin/env bash
# Cloud first-run acceptance. Does not touch production, Pi, or real credentials.
set -euo pipefail
MODE="${1:---quick}"
if [[ "$MODE" != "--quick" && "$MODE" != "--full" ]]; then
  echo "usage: bash scripts/agent-protocol/cloud-preflight.sh [--quick|--full]" >&2
  exit 2
fi
cd "$(dirname "$0")/../.."
for executable in git node go pnpm docker; do
  if ! command -v "$executable" >/dev/null; then
    echo "BLOCKED: required command not found: $executable" >&2
    exit 2
  fi
done
echo "Tool versions: node=$(node --version) go=$(go version) pnpm=$(pnpm --version)"
node --test scripts/agent-protocol/qa-verdict.test.mjs
docker compose version
docker info >/dev/null || { echo "BLOCKED: Docker daemon is not reachable" >&2; exit 2; }
if [[ "$MODE" == "--quick" ]]; then
  echo "PASS: static protocol checks and Docker preflight. Run --full in a disposable Cloud checkout."
  exit 0
fi
if [[ -e .env.qa || -e .env.qa.local ]]; then
  echo "BLOCKED: --full requires an isolated checkout without existing .env.qa files" >&2
  exit 2
fi
if docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml ps -q 2>/dev/null | grep -q .; then
  echo "BLOCKED: offbook-qa stack already exists; refusing to replace it" >&2
  exit 2
fi
cp .env.qa.example .env.qa
printf 'SESSION_SECRET=%s\nQA_SECRET=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" >> .env.qa
cleanup() {
  docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml down -v >/dev/null 2>&1 || true
  rm -f .env.qa
}
trap cleanup EXIT
docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml up -d --build
ready=0
for _ in $(seq 1 60); do
  if curl -fsS http://localhost:18000/api/v1/health >/dev/null 2>&1; then ready=1; break; fi
  sleep 2
done
if [[ "$ready" -ne 1 ]]; then
  echo "FAIL: QA backend not healthy" >&2
  docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml logs --tail 80 >&2
  exit 1
fi
pnpm --dir acceptance install --frozen-lockfile
PLAYWRIGHT_BROWSERS_PATH=.cache/ms-playwright pnpm --dir acceptance exec playwright install chromium
OFFBOOK_ROLE=qa node acceptance/fixtures/bootstrap.mjs
OFFBOOK_ROLE=qa command make qa-smoke
echo "PASS: ephemeral QA Compose + browser smoke completed"
