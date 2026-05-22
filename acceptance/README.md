# Offbook Acceptance Tests

Acceptance tests are manual QA suites that run against the isolated `offbook-qa`
compose stack. They are separate from unit tests and `backend/ make verify`.

## Layout

- `fixtures/` provisions reusable QA personas and shared helpers.
- `auth/` contains opt-in cold-start setup and invite acceptance checks.
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

Cold-start auth/setup checks intentionally reset the QA database to the state
before `/setup/admin`. They are opt-in and ignored by default:

```sh
OFFBOOK_ROLE=qa QA_COLD_START=1 pnpm --dir acceptance exec playwright test auth/setup.cold-start.spec.ts
```

## Environment Loading

Acceptance helpers read `.env.qa` first and `.env.qa.local` second. Helpers that
call `personaPassword()` get that loading for free through `ensureQASecret()`.
Helpers that do not need persona passwords must call `loadQAEnv()` explicitly
before reading `process.env`; `plaid/helper.mjs` does this so Plaid credentials
set only in `.env.qa.local` are visible without manual `export`.

## Persona State Contract

`bootstrap.mjs` creates four shared personas by direct SQL insert. That is fast
and idempotent, but it bypasses `/setup/admin`, `/auth/signup`,
`/auth/signup-with-invite`, and invite acceptance. Do not treat a successful
persona bootstrap as proof that signup works; use the opt-in cold-start suite
for those product paths.

Default acceptance tests must not mutate the four shared personas:

- `qa-admin@offbook.local`
- `qa-contributor@offbook.local`
- `qa-viewer@offbook.local`
- `qa-solo@offbook.local`

Write-heavy suites must create throwaway users with
`createThrowawayUser({ suite })` from `fixtures/state.mjs` and clean them up
with `deleteThrowawayUsers(suite)` in `finally` or `afterEach`. Throwaway emails
use `qa+<suite>-<timestamp>@offbook.local`.

If a suite needs a pristine shared-persona baseline, run:

```sh
OFFBOOK_ROLE=qa node acceptance/fixtures/bootstrap.mjs --reset
```

If a suite needs true first-boot state, run:

```sh
OFFBOOK_ROLE=qa node acceptance/fixtures/cold-start.mjs
```
