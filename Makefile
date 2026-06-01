.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite require-env deploy

# deploy is env-agnostic — ENV_FILE selects the instance (ADR-0016). Each env
# file is the single source of truth for an instance: it declares OFFBOOK_PROJECT
# (compose project name) and OFFBOOK_COMPOSE_FILES (its overlay list, incl. the
# tailscale sidecar) alongside its secrets/TS_HOSTNAME/DB.
#   make deploy                     -> dev   (.env)
#   make deploy ENV_FILE=.env.prod  -> prod
ENV_FILE ?= .env
# TS_AUTHKEY is passed ONLY on first boot (to register the sidecar), never stored
# in the env file. Pass it on the command line: make deploy TS_AUTHKEY=tskey-...
TS_AUTHKEY ?=
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
	@printf '%s\n' '  command make deploy [ENV_FILE=.env]      Deploy/update an instance (.env=dev, ENV_FILE=.env.prod=prod).'
	@printf '%s\n' '                                           First boot only: add TS_AUTHKEY=tskey-... to register the sidecar.'
	@printf '%s\n' ''
	@printf '%s\n' 'Backend-only targets live in backend/Makefile; run them from backend/ with command make <target>.'

verify:
	@$(MAKE) -C backend verify

# require-env: shared guard for bootstrap/deploy — the env file must exist and
# declare OFFBOOK_PROJECT. Keeps an empty $(COMPOSE) (missing -p/-f) from running.
require-env:
	@test -f "$(ENV_FILE)" || { echo "Env file '$(ENV_FILE)' not found — copy .env.example to .env (or .env.prod.example to .env.prod) and fill it in." >&2; exit 2; }
	@test -n "$(OFFBOOK_PROJECT)" || { echo "OFFBOOK_PROJECT missing from $(ENV_FILE) — add OFFBOOK_PROJECT and OFFBOOK_COMPOSE_FILES (see .env.example)." >&2; exit 2; }

# deploy is the single command for an instance — first boot AND updates. It
# stamps the build with the current short SHA (surfaced at GET /health and
# Settings → About, #310), then prunes dangling images (so repeated deploys
# don't fill the disk — matters on the Pi's SD card) and reports /health.
#
# It auto-detects the Tailscale sidecar:
#   • Sidecar already up  -> app-only update: recreate ONLY backend+frontend
#     (--no-deps leaves postgres + the sidecar running). No TS_AUTHKEY needed.
#   • Sidecar not up (first boot) -> bring up the FULL stack. This needs the auth
#     key ONCE to register the sidecar; pass it on the command line and don't
#     store it:  make deploy TS_AUTHKEY=tskey-...
#
# Env-agnostic — the env file picks the instance:
#   command make deploy                                   # dev   (.env)
#   command make deploy ENV_FILE=.env.prod                # prod
#   command make deploy TS_AUTHKEY=tskey-...              # first boot
deploy: require-env
	@SHA="$$(git rev-parse --short HEAD)"; \
	if [ -n "$$($(COMPOSE) ps -q tailscale 2>/dev/null)" ]; then \
		echo "Deploying $(OFFBOOK_PROJECT) @ $$SHA from $(ENV_FILE) (app update; sidecar up)…"; \
		GIT_SHA="$$SHA" $(COMPOSE) up -d --no-deps --build backend frontend; \
	else \
		if $(COMPOSE) config --services 2>/dev/null | grep -qx tailscale && [ -z "$(TS_AUTHKEY)" ]; then \
			echo "First boot needs the Tailscale auth key once — re-run:" >&2; \
			echo "    command make deploy$(if $(filter-out .env,$(ENV_FILE)), ENV_FILE=$(ENV_FILE)) TS_AUTHKEY=tskey-..." >&2; \
			echo "(mint a per-instance key at https://login.tailscale.com/admin/settings/keys — it is not stored)" >&2; \
			exit 2; \
		fi; \
		echo "First boot: bringing up the full $(OFFBOOK_PROJECT) stack @ $$SHA…"; \
		TS_AUTHKEY="$(TS_AUTHKEY)" GIT_SHA="$$SHA" $(COMPOSE) up -d --build; \
	fi
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
