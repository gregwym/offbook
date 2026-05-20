# Offbook Acceptance Tests

Acceptance tests are manual QA suites that run against the isolated `offbook-qa`
compose stack. They are separate from unit tests and `backend/ make verify`.

## Layout

- `fixtures/` provisions reusable QA personas and shared helpers.
- `smoke/` contains the canonical baseline browser smoke suite.
- `plaid/` contains Plaid sandbox acceptance helpers and tests.
- `reports/` is generated output for HTML reports, screenshots, traces, and
  other run artifacts. It is gitignored.

## Driver

Browser acceptance tests use Playwright with headless Chromium. Playwright is
installed through `pnpm --dir acceptance install`; no system browser or `brew`
install is required.

Run from the QA worktree or set `OFFBOOK_ROLE=qa`:

```sh
command make acceptance
```
