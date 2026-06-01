.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite deploy-dev deploy

# Local Tailscale dev stack (ADR-0016). Omits docker-compose.tailscale.yml on
# purpose: the sidecar is authenticated and long-lived, so deploy-dev rebuilds
# only the app images and never touches the sidecar or postgres.
DEV_COMPOSE := docker compose -p offbook-dev -f docker-compose.yml

# Remote deploy target (see the `deploy` target below). DEPLOY_HOST is required;
# DEPLOY_PATH is where the repo is checked out on that host.
DEPLOY_HOST ?=
DEPLOY_PATH ?= ~/offbook

ACCEPTANCE_DIR := acceptance
ACCEPTANCE_BASE_URL ?= http://localhost:15173
ACCEPTANCE_API_URL ?= http://localhost:18000/api/v1
PLAYWRIGHT_BROWSERS_PATH ?= .cache/ms-playwright
QA_SUITE ?=

help:
	@printf '%s\n' 'Offbook root targets:'
	@printf '%s\n' '  command make acceptance          Install browser deps, bootstrap QA personas, run all acceptance suites'
	@printf '%s\n' '  command make qa-smoke            Run the baseline acceptance smoke suite'
	@printf '%s\n' '  command make qa-suite QA_SUITE=plaid  Run one acceptance suite or spec pattern'
	@printf '%s\n' '  command make verify              Run the backend CI mirror from backend/'
	@printf '%s\n' '  command make deploy-dev          Rebuild + recreate the local offbook-dev stack at the current HEAD'
	@printf '%s\n' '  command make deploy DEPLOY_HOST=user@host  Redeploy a remote host over SSH (current HEAD; push first)'
	@printf '%s\n' ''
	@printf '%s\n' 'Backend-only targets live in backend/Makefile; run them from backend/ with command make <target>.'

verify:
	@$(MAKE) -C backend verify

# deploy-dev redeploys the local offbook-dev stack from whatever is checked out.
# It stamps the binary with the current short SHA (surfaced at GET /health and
# Settings → About, #310), recreates only backend + frontend (--no-deps leaves
# postgres + the Tailscale sidecar running), and prints the new /health version.
# Migrations run on backend boot. There is no CD — this is the manual deploy.
deploy-dev:
	@SHA="$$(git rev-parse --short HEAD)"; \
		echo "Building offbook-dev at $$SHA…"; \
		GIT_SHA="$$SHA" $(DEV_COMPOSE) build backend frontend
	@$(DEV_COMPOSE) up -d --no-deps backend frontend
	@printf 'Deployed offbook-dev → '; curl -fsS http://localhost:8000/api/v1/health && echo

# deploy redeploys a REMOTE host over SSH by running deploy-dev *on* that host,
# not by driving a remote Docker socket. Running it remotely means the deploy
# uses the remote's own .env, named volumes, and Docker daemon — so no secrets
# or config from your machine leak into the remote (which DOCKER_HOST=ssh:// +
# `env_file: .env` would cause). It deploys whatever commit YOU have checked out
# locally (parity with deploy-dev), so push it to origin first.
#
# Remote prerequisites (an ADR-0016 self-host box already meets these): the repo
# checked out at DEPLOY_PATH, Docker + make installed, and the offbook-dev stack
# provisioned once (postgres volume + authenticated Tailscale sidecar).
#
#   command make deploy DEPLOY_HOST=user@host [DEPLOY_PATH=/srv/offbook]
deploy:
	@test -n "$(DEPLOY_HOST)" || { echo 'Set DEPLOY_HOST=user@host — e.g. command make deploy DEPLOY_HOST=greg@offbook-host' >&2; exit 2; }
	@SHA="$$(git rev-parse --short HEAD)"; \
		echo "Deploying $$SHA → $(DEPLOY_HOST):$(DEPLOY_PATH) over ssh…"; \
		ssh "$(DEPLOY_HOST)" "set -e; cd $(DEPLOY_PATH) && git fetch --quiet origin && git checkout --quiet $$SHA && make deploy-dev"

acceptance:
	@./scripts/qa-assert-role.sh
	@./scripts/qa-preflight.sh
	@pnpm --dir $(ACCEPTANCE_DIR) install --frozen-lockfile
	@PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_BROWSERS_PATH)" pnpm --dir $(ACCEPTANCE_DIR) exec playwright install chromium
	@node $(ACCEPTANCE_DIR)/fixtures/bootstrap.mjs
	@ACCEPTANCE_BASE_URL="$(ACCEPTANCE_BASE_URL)" ACCEPTANCE_API_URL="$(ACCEPTANCE_API_URL)" PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_BROWSERS_PATH)" pnpm --dir $(ACCEPTANCE_DIR) exec playwright test

qa-smoke:
	@./scripts/qa-assert-role.sh
	@./scripts/qa-preflight.sh
	@ACCEPTANCE_BASE_URL="$(ACCEPTANCE_BASE_URL)" ACCEPTANCE_API_URL="$(ACCEPTANCE_API_URL)" PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_BROWSERS_PATH)" pnpm --dir $(ACCEPTANCE_DIR) exec playwright test smoke

qa-suite:
	@./scripts/qa-assert-role.sh
	@./scripts/qa-preflight.sh
	@test -n "$(QA_SUITE)" || (echo 'Set QA_SUITE, for example: command make qa-suite QA_SUITE=plaid' >&2; exit 2)
	@ACCEPTANCE_BASE_URL="$(ACCEPTANCE_BASE_URL)" ACCEPTANCE_API_URL="$(ACCEPTANCE_API_URL)" PLAYWRIGHT_BROWSERS_PATH="$(PLAYWRIGHT_BROWSERS_PATH)" pnpm --dir $(ACCEPTANCE_DIR) exec playwright test "$(QA_SUITE)"
