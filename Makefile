.DEFAULT_GOAL := help

.PHONY: help verify acceptance qa-smoke qa-suite

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
	@printf '%s\n' ''
	@printf '%s\n' 'Backend-only targets live in backend/Makefile; run them from backend/ with command make <target>.'

verify:
	@$(MAKE) -C backend verify

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
