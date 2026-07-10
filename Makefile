.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite require-env ensure-env pre-deploy-backup deploy deployed-sha down teardown auto-deploy-install auto-deploy-uninstall

# ─── Deploy configuration (ADR-0016) ─────────────────────────────────────────
# Near-zero config by convention. The common case — one instance, behind
# Tailscale, tracking main — needs NO compose-plumbing env vars:
#   make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook   # first boot
#   make deploy                                            # updates
#
# FLAVOR selects a deployment shape in one word — it sets the compose project
# name, overlay list, and default env file together:
#   make deploy              -> dev  (project offbook,      .env)
#   make deploy FLAVOR=prod  -> prod (project offbook-prod, .env.prod)
FLAVOR ?= dev

ifeq ($(FLAVOR),prod)
DEFAULT_PROJECT := offbook-prod
DEFAULT_FILES   := docker-compose.yml docker-compose.prod.yml docker-compose.tailscale.yml
DEFAULT_ENV     := .env.prod
else
DEFAULT_PROJECT := offbook
DEFAULT_FILES   := docker-compose.yml docker-compose.tailscale.yml
DEFAULT_ENV     := .env
endif

# The env file is the single source of truth for an instance's SECRETS only.
# ENV_FILE overrides the flavor default.
ENV_FILE ?= $(DEFAULT_ENV)

# TS_* are Tailscale IDENTITY, consumed ONLY when the sidecar is first created.
# Pass them once on the first-boot command line; they are never stored.
TS_AUTHKEY ?=
TS_HOSTNAME ?=

# Project + overlays come from convention. An env file MAY still override them
# (escape hatch for exotic multi-instance hosts) via OFFBOOK_PROJECT /
# OFFBOOK_COMPOSE_FILES — custom vars, not COMPOSE_FILE, so stray
# `docker compose` calls aren't affected. Absent -> the flavor default.
OFFBOOK_PROJECT := $(or $(shell sed -n 's/^OFFBOOK_PROJECT=//p' $(ENV_FILE) 2>/dev/null),$(DEFAULT_PROJECT))
OFFBOOK_FILES := $(or $(shell sed -n 's/^OFFBOOK_COMPOSE_FILES=//p' $(ENV_FILE) 2>/dev/null | tr ':' ' '),$(DEFAULT_FILES))
# Compose invocation for the selected instance: explicit project + overlays,
# with the env file as the interpolation/secret source.
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
	@printf '%s\n' '  command make deploy [FLAVOR=prod]        Deploy/update an instance (default dev). First boot only:'
	@printf '%s\n' '                                           add TS_AUTHKEY=tskey-... TS_HOSTNAME=<name> to register the sidecar.'
	@printf '%s\n' '  command make down [FLAVOR=prod]          Stop an instance (data preserved).'
	@printf '%s\n' '  command make teardown [FLAVOR=prod]      Destroy an instance: containers + volumes (DB + tailnet identity).'
	@printf '%s\n' '  command make auto-deploy-install         Install the user-level systemd poll-and-redeploy timer.'
	@printf '%s\n' '  command make auto-deploy-uninstall       Remove the auto-deploy timer.'
	@printf '%s\n' ''
	@printf '%s\n' 'Backend-only targets live in backend/Makefile; run them from backend/ with command make <target>.'

verify:
	@$(MAKE) -C backend verify

# require-env: the env file must exist. Project/overlays now have convention
# defaults, so nothing else is required.
require-env:
	@test -f "$(ENV_FILE)" || { echo "Env file '$(ENV_FILE)' not found. Run 'command make deploy' to create it, or copy $(ENV_FILE).example." >&2; exit 2; }

# ensure-env: make first boot zero-config. Create the env file from its example
# if missing, then generate SESSION_SECRET if it has no value. Idempotent and
# NON-rotating — an existing secret is never touched (rotating it invalidates
# every session and makes stored Claude keys unrecoverable; see .env.example).
ensure-env:
	@if [ ! -f "$(ENV_FILE)" ]; then \
		if [ -f "$(ENV_FILE).example" ]; then \
			cp "$(ENV_FILE).example" "$(ENV_FILE)"; \
			echo "Created $(ENV_FILE) from $(ENV_FILE).example"; \
		else \
			: > "$(ENV_FILE)"; \
			echo "Created empty $(ENV_FILE)"; \
		fi; \
	fi
	@if ! grep -qE '^SESSION_SECRET=.+' "$(ENV_FILE)"; then \
		secret="$$(openssl rand -hex 32)"; tmp="$$(mktemp)"; \
		if grep -qE '^SESSION_SECRET=' "$(ENV_FILE)"; then \
			sed "s|^SESSION_SECRET=.*|SESSION_SECRET=$$secret|" "$(ENV_FILE)" > "$$tmp" && mv "$$tmp" "$(ENV_FILE)"; \
		else \
			cp "$(ENV_FILE)" "$$tmp" && printf 'SESSION_SECRET=%s\n' "$$secret" >> "$$tmp" && mv "$$tmp" "$(ENV_FILE)"; \
		fi; \
		echo "Generated a SESSION_SECRET into $(ENV_FILE)"; \
	fi

# deploy is the single command for an instance — first boot AND updates. It
# stamps the build with the current short SHA (surfaced at GET /health and
# Settings → About, #310), then prunes dangling images (so repeated deploys
# don't fill the disk — matters on the Pi's SD card) and reports /health.
#
# It auto-detects the Tailscale sidecar:
#   • Sidecar already up  -> app-only update: recreate ONLY backend+frontend
#     (--no-deps leaves postgres + the sidecar running). No TS_* needed.
#   • Sidecar not up (first boot) -> bring up the FULL stack. This needs the
#     Tailscale identity ONCE to register the node; pass it on the command line
#     and don't store it:  make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook
#
# FLAVOR picks the instance:
#   command make deploy                                          # dev
#   command make deploy FLAVOR=prod                              # prod
#   command make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook # first boot
# pre-deploy-backup: migration-safety hook (#358). Every deploy that could run a
# migration on a data-bearing instance takes a backup FIRST — down-migrations are
# a dev/rollback tool, not a data-recovery tool; backups are the recovery path.
# The backup implementation is the M13 `backup` target (#357). Until that target
# exists this is a loud no-op so deploy still works, but a data-bearing instance
# MUST NOT run migrations without it once #357 lands. First-boot instances have
# no data yet, so a warning there is harmless.
pre-deploy-backup:
	@if $(MAKE) -n backup >/dev/null 2>&1; then \
		echo "Taking pre-migration backup (migration safety, #358)…"; \
		FLAVOR="$(FLAVOR)" ENV_FILE="$(ENV_FILE)" $(MAKE) backup; \
	else \
		echo "⚠️  No 'backup' target yet (#357) — skipping pre-migration backup." >&2; \
		echo "   Do not run migrations on an instance holding real data until #357 lands." >&2; \
	fi

deploy: ensure-env pre-deploy-backup
	@SHA="$$(git rev-parse --short HEAD)"; \
	if [ -n "$$($(COMPOSE) ps -q tailscale 2>/dev/null)" ]; then \
		echo "Deploying $(OFFBOOK_PROJECT) @ $$SHA from $(ENV_FILE) (app update; sidecar up)…"; \
		GIT_SHA="$$SHA" $(COMPOSE) up -d --no-deps --build backend frontend; \
	else \
		if $(COMPOSE) config --services 2>/dev/null | grep -qx tailscale; then \
			missing=""; \
			[ -z "$(TS_AUTHKEY)" ] && missing="TS_AUTHKEY"; \
			[ -z "$(TS_HOSTNAME)" ] && missing="$$missing TS_HOSTNAME"; \
			if [ -n "$$missing" ]; then \
				echo "First boot needs the Tailscale identity once —$$missing not set. Re-run:" >&2; \
				echo "    command make deploy$(if $(filter-out dev,$(FLAVOR)), FLAVOR=$(FLAVOR)) TS_AUTHKEY=tskey-... TS_HOSTNAME=<name>" >&2; \
				echo "(mint a per-instance key at https://login.tailscale.com/admin/settings/keys — neither value is stored)" >&2; \
				exit 2; \
			fi; \
		fi; \
		echo "First boot: bringing up the full $(OFFBOOK_PROJECT) stack @ $$SHA as '$(TS_HOSTNAME)'…"; \
		TS_AUTHKEY="$(TS_AUTHKEY)" TS_HOSTNAME="$(TS_HOSTNAME)" GIT_SHA="$$SHA" $(COMPOSE) up -d --build; \
	fi
	@printf 'Pruning dangling images… '; docker image prune -f | tail -1
	@printf 'Deployed $(OFFBOOK_PROJECT) → '; \
		$(COMPOSE) exec -T backend wget -qO- http://localhost:8000/api/v1/health 2>/dev/null && echo \
		|| echo '(backend still starting — check /health shortly)'

# deployed-sha: the build SHA the running backend reports at /health (#310), via
# a container exec so it works whether or not host ports are published (prod
# binds none). Empty when the backend is down/unreachable. Used by auto-deploy
# to decide "is the running build current?" without depending on host ports.
deployed-sha:
	@$(COMPOSE) exec -T backend wget -qO- http://localhost:8000/api/v1/health 2>/dev/null \
		| grep -oE '"version":"[^"]+"' | cut -d'"' -f4 || true

# down: stop the instance, keep volumes (DB + tailnet identity survive).
down: require-env
	@echo "Stopping $(OFFBOOK_PROJECT) (data preserved)…"
	$(COMPOSE) down

# teardown: destroy the instance INCLUDING volumes — postgres data AND the
# tailscale_state volume (so the node de-registers). Irreversible. Guarded by a
# confirmation prompt; pass FORCE=1 to skip it (for scripts).
teardown: require-env
	@echo "⚠️  This destroys $(OFFBOOK_PROJECT): containers + volumes (postgres data AND the Tailscale node identity)."
	@if [ -z "$(FORCE)" ]; then \
		printf 'Type the project name (%s) to confirm: ' "$(OFFBOOK_PROJECT)"; \
		read ans; [ "$$ans" = "$(OFFBOOK_PROJECT)" ] || { echo "Aborted."; exit 1; }; \
	fi
	$(COMPOSE) down -v
	@echo "Volumes dropped. Remove the stale node from https://login.tailscale.com/admin/machines if it lingers."
	@echo "To also remove the auto-deploy timer: command make auto-deploy-uninstall$(if $(filter-out dev,$(FLAVOR)), FLAVOR=$(FLAVOR))"

# auto-deploy-install / -uninstall: manage the user-level systemd poll-and-
# redeploy timer for this FLAVOR. No file editing, no sudo. See
# infra/auto-deploy/README.md.
auto-deploy-install:
	@OFFBOOK_FLAVOR="$(FLAVOR)" infra/auto-deploy/install.sh

auto-deploy-uninstall:
	@OFFBOOK_FLAVOR="$(FLAVOR)" infra/auto-deploy/uninstall.sh

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
