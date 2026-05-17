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

.PHONY: help dev stop restart smoke test test-v lint vet build clean routes logs

help:
	@echo "make dev        - start backend (stops any previous instance first)"
	@echo "make stop       - kill any process bound to :$(PORT)"
	@echo "make restart    - stop + dev"
	@echo "make smoke      - dev + wait for /health + report URL"
	@echo "make test       - go test ./..."
	@echo "make test-v     - go test -v ./..."
	@echo "make lint       - golangci-lint via Docker"
	@echo "make vet        - go vet ./..."
	@echo "make build      - go build ./..."
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
	@go run ./cmd/server > $(LOGFILE) 2>&1 & echo $$! > $(PIDFILE)
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

test:
	@go test ./...

test-v:
	@go test -v ./...

lint:
	@docker run --rm -v "$$PWD":/app -w /app golangci/golangci-lint:latest-alpine golangci-lint run ./...

vet:
	@go vet ./...

build:
	@go build ./...

routes:
	@grep "GIN-debug" $(LOGFILE) || echo "no routes logged yet (try: make smoke)"

logs:
	@tail -f $(LOGFILE)

clean: stop
	@/bin/rm -f $(PIDFILE) $(LOGFILE)
