.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite require-env bootstrap deploy

# bootstrap/deploy are env-agnostic — ENV_FILE selects the instance (ADR-0016).
# Each env file is the single source of truth for an instance: it declares
# OFFBOOK_PROJECT (compose project name) and OFFBOOK_COMPOSE_FILES (its overlay
# list, incl. the tailscale sidecar) alongside its secrets/TS_*/DB.
#   make deploy                  -> dev   (.env)
#   make deploy ENV_FILE=.env.prod  -> prod
ENV_FILE ?= .env
# Read project + overlay files from the env file (custom OFFBOOK_* vars, not
# COMPOSE_FILE, so stray `docker compose` calls aren't affected). Missing file
# -> empty; the require-env prerequisite turns that into a clear error.
OFFBOOK_PROJECT := $(shell sed -n 's/^OFFBOOK_PROJECT=//p' $(ENV_FILE) 2>/dev/null)
OFFBOOK_FILES := $(shell sed -n 's/^OFFBOOK_COMPOSE_FILES=//p' $(ENV_FILE) 2>/dev/null | tr ':' ' ')
# Compose invocation for the selected instance: explicit project + overlays from
# the env file, with the env file as the interpolation/secret source.
COMPOSE := docker compose --env-file $(ENV_FILE) -p $(OFFBOOK_PROJECT) $(foreach f,$(OFFBOOK_FILES),-f $(f))

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
	@printf '%s\n' '  command make bootstrap [ENV_FILE=.env]   First boot: full stack (postgres + sidecar), stamped, per the env file'
	@printf '%s\n' '  command make deploy    [ENV_FILE=.env]   Rebuild + recreate app, prune; .env=dev, ENV_FILE=.env.prod=prod'
	@printf '%s\n' ''
	@printf '%s\n' 'Backend-only targets live in backend/Makefile; run them from backend/ with command make <target>.'

verify:
	@$(MAKE) -C backend verify

# require-env: shared guard for bootstrap/deploy — the env file must exist and
# declare OFFBOOK_PROJECT. Keeps an empty $(COMPOSE) (missing -p/-f) from running.
require-env:
	@test -f "$(ENV_FILE)" || { echo "Env file '$(ENV_FILE)' not found — copy .env.example to .env (or .env.prod.example to .env.prod) and fill it in." >&2; exit 2; }
	@test -n "$(OFFBOOK_PROJECT)" || { echo "OFFBOOK_PROJECT missing from $(ENV_FILE) — add OFFBOOK_PROJECT and OFFBOOK_COMPOSE_FILES (see .env.example)." >&2; exit 2; }

# bootstrap is the ONE-TIME first boot for an instance: it brings up the full
# stack — postgres + backend + frontend + the Tailscale sidecar — as declared by
# the env file's OFFBOOK_COMPOSE_FILES, stamped with the current SHA so /health
# reports a real commit from the start (not "dev"). Compose fail-fasts if the
# env file is missing TS_AUTHKEY/TS_HOSTNAME/SESSION_SECRET. After this, use
# deploy (or the auto-deploy timer) for updates.
#   command make bootstrap                    # dev   (.env)
#   command make bootstrap ENV_FILE=.env.prod # prod
bootstrap: require-env
	@SHA="$$(git rev-parse --short HEAD)"; \
		echo "Bootstrapping $(OFFBOOK_PROJECT) @ $$SHA from $(ENV_FILE)…"; \
		GIT_SHA="$$SHA" $(COMPOSE) up -d --build
	@printf 'Bootstrapped $(OFFBOOK_PROJECT) → '; \
		$(COMPOSE) exec -T backend wget -qO- http://localhost:8000/api/v1/health 2>/dev/null && echo \
		|| echo '(backend still starting — check /health shortly)'

# deploy updates an already-bootstrapped instance from whatever is checked out:
# it stamps the binary with the current short SHA (surfaced at GET /health and
# Settings → About, #310) and recreates only backend + frontend (--no-deps leaves
# postgres + the sidecar running). Migrations run on backend boot.
#
# It then prunes DANGLING images — the untagged layers orphaned when the :latest
# tag moved to the new build — so repeated deploys don't fill the disk (matters
# on the Pi's SD card). Dangling-only: tagged / in-use images are never touched.
#
# Env-agnostic — the env file picks the instance:
#   command make deploy                    # dev   (.env)
#   command make deploy ENV_FILE=.env.prod # prod
deploy: require-env
	@SHA="$$(git rev-parse --short HEAD)"; \
		echo "Deploying $(OFFBOOK_PROJECT) @ $$SHA from $(ENV_FILE)…"; \
		GIT_SHA="$$SHA" $(COMPOSE) up -d --no-deps --build backend frontend
	@printf 'Pruning dangling images… '; docker image prune -f | tail -1
	@printf 'Deployed $(OFFBOOK_PROJECT) → '; \
		$(COMPOSE) exec -T backend wget -qO- http://localhost:8000/api/v1/health 2>/dev/null && echo \
		|| echo '(backend still starting — check /health shortly)'

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
