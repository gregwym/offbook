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

For discoverability, the repo root exposes:

```sh
command make help
command make qa-smoke
command make qa-suite QA_SUITE=plaid
```

Run a suite or spec directly when you need Playwright flags:

```sh
pnpm --dir acceptance exec playwright test plaid
OFFBOOK_ROLE=qa pnpm --dir acceptance exec playwright test smoke/baseline.spec.ts:19
```

## Environment Loading

Acceptance helpers read `.env.qa` first and `.env.qa.local` second. Helpers that
call `personaPassword()` get that loading for free through `ensureQASecret()`.
Helpers that do not need persona passwords must call `loadQAEnv()` explicitly
before reading `process.env`; `plaid/helper.mjs` does this so Plaid credentials
set only in `.env.qa.local` are visible without manual `export`.
