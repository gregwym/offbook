# Backend dev workflow.
#
# Why these targets exist:
# - `go run ./cmd/server &` forks a `go run` supervisor plus the compiled
#   server. Killing the supervisor PID doesn't always reap the child, so
#   the next session inherits a process bound to :8000. `make stop` kills
#   by *port* (always reliable).
# - The autonomous-development allowlist matches `Bash(make *)`, so these
#   targets run without approval prompts.

PORT       ?= 8000
PIDFILE    ?= /tmp/offbook-server.pid
LOGFILE    ?= /tmp/offbook-server.log
HEALTH_URL  = http://localhost:$(PORT)/api/v1/health

# Env isolation (#183): dev / test / prod each resolve their own DB through
# internal/config. `make dev` runs against APP_ENV=dev (offbook_dev DB);
# `make test` runs against APP_ENV=test (offbook_test DB). Override with
# DATABASE_URL on either if you need to point at a non-local Postgres.
TEST_DATABASE_URL ?= postgres://offbook:offbook@localhost:5432/offbook_test?sslmode=disable

.PHONY: help dev stop restart smoke test test-v test-fast lint vet build verify contract-check schema schema-check clean routes logs

# Checked-in schema report (#304). Regenerated from the migrated DB by
# cmd/schema-report; CI enforces it via schema-check.
SCHEMA_FILE ?= ../docs/db/schema.md

help:
	@echo "make dev        - start backend (stops any previous instance first)"
	@echo "make stop       - kill any process bound to :$(PORT)"
	@echo "make restart    - stop + dev"
	@echo "make smoke      - dev + wait for /health + report URL"
	@echo "make test       - go test -race -p 1 ./... (matches CI)"
	@echo "make test-fast  - go test ./... (no -race, parallel; for tight dev loops)"
	@echo "make test-v     - go test -race -p 1 -v ./..."
	@echo "make lint       - golangci-lint via Docker"
	@echo "make vet        - go vet ./..."
	@echo "make build      - go build ./..."
	@echo "make verify     - full CI mirror: contract-check, backend vet+test, frontend lint+build"
	@echo "make contract-check - assert every frontend apiClient URL maps to a registered backend route (#273)"
	@echo "make schema     - regenerate $(SCHEMA_FILE) from the migrated DB (#304)"
	@echo "make schema-check - fail if $(SCHEMA_FILE) is stale vs the migrations (CI mirror)"
	@echo "make routes     - print routes from the last server log"
	@echo "make logs       - tail $(LOGFILE)"

# Always kill by port first. If nothing's listening, lsof exits non-zero and
# xargs gets an empty input; the `|| true` keeps make happy either way.
stop:
	@lsof -ti:$(PORT) | xargs -r kill 2>/dev/null || true
	@sleep 1
	@/bin/rm -f $(PIDFILE) $(LOGFILE)
	@echo "stopped (port $(PORT) free)"

dev: stop
	@APP_ENV=dev go run ./cmd/server > $(LOGFILE) 2>&1 & echo $$! > $(PIDFILE)
	@echo "started (pid $$(cat $(PIDFILE)), log $(LOGFILE))"

restart: stop dev

# Start the server and wait until it answers /health (up to ~10s). Useful as
# a precondition for ad-hoc curl smoke tests. Fails loud if the server never
# becomes ready.
smoke: dev
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		if curl -sf $(HEALTH_URL) >/dev/null; then \
			echo "ready at $(HEALTH_URL)"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "server did not become ready in 10s; see $(LOGFILE)"; \
	tail -n 50 $(LOGFILE); \
	exit 1

# -race -p 1 mirrors CI exactly. Integration tests share one DATABASE_URL,
# so parallel packages step on each other's rows. Use `make test-fast` for
# tight dev loops on a single package, but `make test` is what CI sees.
#
# APP_ENV=test + DATABASE_URL=offbook_test guarantees the test suite cannot
# touch the dev DB (#183). Migrations run first so a fresh checkout works
# without manual setup.
test:
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go run ./cmd/migrate up >/dev/null
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -p 1 ./...

test-fast:
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...

test-v:
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -p 1 -v ./...

lint:
	@docker run --rm -v "$$PWD":/app -w /app golangci/golangci-lint:latest-alpine golangci-lint run ./...

vet:
	@go vet ./...

build:
	@go build ./...

# Full CI mirror. Run before opening a PR if you want to catch CI failures
# locally. Skips frontend if ../frontend doesn't exist (early-milestone safety).
verify:
	@echo "== contract check =="
	@$(MAKE) contract-check
	@echo "== backend vet =="
	@go vet ./...
	@echo "== backend lint =="
	@$(MAKE) lint
	@echo "== backend test =="
	@$(MAKE) test
	@echo "== schema check =="
	@$(MAKE) schema-check
	@if [ -d ../frontend ] && [ -f ../frontend/package.json ]; then \
		echo "== frontend lint =="; \
		(cd ../frontend && pnpm lint) || exit 1; \
		echo "== frontend build =="; \
		(cd ../frontend && pnpm build) || exit 1; \
	else \
		echo "(skipping frontend — no ../frontend)"; \
	fi
	@echo "verify OK"

routes:
	@grep "GIN-debug" $(LOGFILE) || echo "no routes logged yet (try: make smoke)"

# Asserts every apiClient.<method>('<path>') in frontend/src/api/*.ts maps
# to a registered backend route. Sub-second, no DB, no compose stack.
# Catches the bug class behind #266 / #268 — see epic #270.
contract-check:
	@go run ./cmd/contract-check

# Regenerate the checked-in schema report from the migrated DB. Migrates
# offbook_test first (idempotent; same DB `make test` uses) so the report
# always reflects the head migration. The report is DB-name-independent, so
# this produces the same bytes locally and in CI. If a stale test DB blocks
# `migrate up` across branches, recreate it (DROP/CREATE offbook_test).
schema:
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go run ./cmd/migrate up >/dev/null
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go run ./cmd/schema-report --out $(SCHEMA_FILE)

# Fail if $(SCHEMA_FILE) doesn't match the migrated schema — i.e. someone added
# a migration without running `make schema`. Mirrors the CI schema-check job.
schema-check:
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go run ./cmd/migrate up >/dev/null
	@APP_ENV=test DATABASE_URL='$(TEST_DATABASE_URL)' go run ./cmd/schema-report --check --out $(SCHEMA_FILE)

logs:
	@tail -f $(LOGFILE)

clean: stop
	@/bin/rm -f $(PIDFILE) $(LOGFILE)
