.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite require-env ensure-env pre-deploy-backup deploy deployed-sha down teardown auto-deploy-install auto-deploy-uninstall backup restore backup-verify backup-list backup-install backup-uninstall delivery-install delivery-uninstall

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

# ─── Backup configuration (#357) ─────────────────────────────────────────────
# Dumps land here, deliberately OUTSIDE the postgres data volume (so losing the
# volume never loses the backups). Per-project so dev and prod don't mix.
# Retention: keep the newest N dailies + one dump per week for M weeks.
BACKUP_DIR ?= backups/$(OFFBOOK_PROJECT)
BACKUP_KEEP_DAILY ?= 7
BACKUP_KEEP_WEEKLY ?= 4
# Exported to the infra/backup scripts so they can reach THIS instance's
# postgres container without re-deriving the compose invocation.
BACKUP_ENV = OFFBOOK_COMPOSE='$(COMPOSE)' OFFBOOK_PROJECT='$(OFFBOOK_PROJECT)' \
	BACKUP_DIR='$(BACKUP_DIR)' BACKUP_KEEP_DAILY='$(BACKUP_KEEP_DAILY)' \
	BACKUP_KEEP_WEEKLY='$(BACKUP_KEEP_WEEKLY)'

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
	@printf '%s\n' '  command make deploy GIT_REF=<sha>        Pin/roll back to a prior commit (see docs/ops/deploy-rollback.md).'
	@printf '%s\n' '  command make down [FLAVOR=prod]          Stop an instance (data preserved).'
	@printf '%s\n' '  command make teardown [FLAVOR=prod]      Destroy an instance: containers + volumes (DB + tailnet identity).'
	@printf '%s\n' '  command make auto-deploy-install         Install the user-level systemd poll-and-redeploy timer.'
	@printf '%s\n' '  command make auto-deploy-uninstall       Remove the auto-deploy timer.'
	@printf '%s\n' '  command make backup [FLAVOR=prod]        Dump the DB now (+ prune, + off-host if configured).'
	@printf '%s\n' '  command make backup-verify               Prove the latest dump restores (into a scratch DB).'
	@printf '%s\n' '  command make restore BACKUP=<file>       Recover the DB from a dump (destructive; typed confirm).'
	@printf '%s\n' '  command make backup-list                 List the dumps on hand for this instance.'
	@printf '%s\n' '  command make backup-install              Install the user-level nightly backup timer.'
	@printf '%s\n' '  command make backup-uninstall            Remove the nightly backup timer.'
	@printf '%s\n' '  command make delivery-install            Install the launchd autonomous-delivery loop (macOS).'
	@printf '%s\n' '  command make delivery-uninstall          Remove the delivery-loop LaunchAgent.'
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

# deploy is the single command for an instance — first boot, updates, AND
# rollback/pin. It stamps the build with the current short SHA (surfaced at
# GET /health and Settings → About, #310), then prunes dangling images (so
# repeated deploys don't fill the disk — matters on the Pi's SD card) and
# runs a post-deploy smoke check (#361, see below).
#
# It auto-detects the Tailscale sidecar:
#   • Sidecar already up  -> app-only update: recreate ONLY backend+frontend
#     (--no-deps leaves postgres + the sidecar running). No TS_* needed.
#   • Sidecar not up (first boot) -> bring up the FULL stack. This needs the
#     Tailscale identity ONCE to register the node; pass it on the command line
#     and don't store it:  make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook
#
# GIT_REF pins/rolls back the deploy to a specific commit/tag instead of
# whatever HEAD currently is (#361; see docs/ops/deploy-rollback.md). Compose
# builds from the working tree, so "deploy an older build" is just "check out
# an older commit, then deploy" — scripted here rather than inventing an
# image registry. Refuses if the checkout has uncommitted changes (would
# silently fold them into the "rollback"). This does NOT touch the database —
# see the runbook for when a restore is needed instead.
#   command make deploy GIT_REF=<sha-or-tag> [FLAVOR=prod]
#
# FLAVOR picks the instance:
#   command make deploy                                          # dev
#   command make deploy FLAVOR=prod                              # prod
#   command make deploy TS_AUTHKEY=tskey-... TS_HOSTNAME=offbook # first boot
# pre-deploy-backup: migration-safety hook (#358), backed by the #357 backup
# target. Every deploy of a data-bearing instance takes a backup FIRST — a deploy
# can run a migration, and down-migrations are a dev/rollback tool, not a
# data-recovery tool; backups are the recovery path. On FIRST boot the postgres
# service isn't up yet (nothing to back up), so this skips cleanly; on an update
# it backs up and a failure aborts the deploy (don't migrate what you can't restore).
pre-deploy-backup:
	@if [ -n "$$($(COMPOSE) ps -q postgres 2>/dev/null)" ]; then \
		echo "Taking pre-migration backup (migration safety, #357/#358)…"; \
		FLAVOR="$(FLAVOR)" ENV_FILE="$(ENV_FILE)" $(MAKE) backup; \
	else \
		echo "First boot / postgres not running — nothing to back up before deploy."; \
	fi

deploy: ensure-env pre-deploy-backup
	@if [ -n "$(GIT_REF)" ]; then \
		git fetch --quiet origin >/dev/null 2>&1 || true; \
		git rev-parse --verify --quiet "$(GIT_REF)^{commit}" >/dev/null 2>&1 || { \
			echo "GIT_REF '$(GIT_REF)' is not a known commit/tag in this checkout (try 'git fetch origin' first)." >&2; exit 2; \
		}; \
		if ! git diff --quiet HEAD --; then \
			echo "This deploy checkout has uncommitted changes — commit or stash before pinning/rolling back." >&2; exit 2; \
		fi; \
		echo "Pinning $(OFFBOOK_PROJECT) to $(GIT_REF)."; \
		echo "If the auto-deploy timer is active for this flavor, pause it first — it will fast-forward back to origin/main on its next tick:"; \
		echo "    systemctl --user disable --now offbook-deploy@$(FLAVOR).timer"; \
		git checkout --quiet "$(GIT_REF)"; \
	fi
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
	@SHA="$$(git rev-parse --short HEAD)"; \
		OFFBOOK_COMPOSE='$(COMPOSE)' OFFBOOK_PROJECT='$(OFFBOOK_PROJECT)' \
		infra/deploy/post-deploy-smoke.sh "$$SHA"
	@echo "Deployed $(OFFBOOK_PROJECT) @ $$(git rev-parse --short HEAD)."

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

# ─── Backups & restore (#357) ────────────────────────────────────────────────
# backup: dump this instance's DB (pg_dump -Fc) to $(BACKUP_DIR), prune old
# dumps, and (if BACKUP_REMOTE is set) copy off-host. Runs nightly via the
# offbook-backup@<flavor>.timer; run it by hand any time before risky changes.
backup: require-env
	@$(BACKUP_ENV) infra/backup/backup.sh

# restore: recover the DB from a dump. DESTRUCTIVE — drops & recreates the live
# database. Guarded by a typed confirmation (skip with FORCE=1).
#   command make restore BACKUP=backups/offbook/offbook-YYYYmmdd-HHMMSS.dump
restore: require-env
	@test -n "$(BACKUP)" || { echo "Set BACKUP=<file>. List them: command make backup-list" >&2; exit 2; }
	@test -f "$(BACKUP)" || { echo "Backup file '$(BACKUP)' not found." >&2; exit 2; }
	@if [ -z "$(FORCE)" ]; then \
		printf '⚠️  This REPLACES the %s database with %s (all current data is lost).\n' "$(OFFBOOK_PROJECT)" "$(BACKUP)"; \
		printf 'Type the project name (%s) to confirm: ' "$(OFFBOOK_PROJECT)"; \
		read ans; [ "$$ans" = "$(OFFBOOK_PROJECT)" ] || { echo "Aborted."; exit 1; }; \
	fi
	@$(BACKUP_ENV) infra/backup/restore.sh "$(BACKUP)"

# backup-verify: prove the LATEST dump restores — into a throwaway scratch DB,
# never the live one. An unrestored backup is not a backup. Part of the nightly run.
backup-verify: require-env
	@$(BACKUP_ENV) infra/backup/verify.sh $(BACKUP)

# backup-list: show the dumps on hand for this instance, newest last.
backup-list:
	@ls -lh "$(BACKUP_DIR)"/$(OFFBOOK_PROJECT)-*.dump 2>/dev/null || echo "No backups yet in $(BACKUP_DIR)."

# backup-install / -uninstall: manage the user-level nightly backup timer for
# this FLAVOR (mirrors auto-deploy). No file editing, no sudo. See infra/backup/README.md.
backup-install:
	@OFFBOOK_FLAVOR="$(FLAVOR)" infra/backup/install.sh

backup-uninstall:
	@OFFBOOK_FLAVOR="$(FLAVOR)" infra/backup/uninstall.sh

# delivery-install / -uninstall: manage the user-level launchd agent that runs
# the autonomous delivery loop headless on the owner's Mac. See
# infra/delivery-loop/README.md and docs/dev/autonomous-delivery.md § Durability.
delivery-install:
	@infra/delivery-loop/install.sh

delivery-uninstall:
	@infra/delivery-loop/uninstall.sh

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
