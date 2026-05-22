# Offbook QA Workflow

Offbook QA is a manually triggered role. The same agent session should not both implement a change and certify it as QA. A developer agent may run focused checks while building, but autonomous QA starts only when the user explicitly asks for it, for example: "be QA", "run QA", "QA issue #123", or "QA since the last reviewed commit."

QA work is evidence gathering, issue filing, and acceptance-test development. It must not make product code changes unless the user explicitly switches the session from QA to development. Browser automation uses Playwright with headless Chromium.

## Product Contract

Treat these files as the QA contract before testing behavior:

- `docs/ROADMAP.md` for the shipped milestone goals.
- `docs/designs/App Hierarchy v4.html` for locked information architecture, privacy, scope, and lifecycle rules.
- `docs/designs/Offbook Hi-Fi v1.html` for intended page composition and visual direction.
- `AGENTS.md` plus scoped rules under `.claude/rules/`.

The wireframe is not only visual. It defines product invariants:

- Personal and household scopes are mutually exclusive in the sidebar.
- Household scope is the default after login when the user belongs to a household.
- Personal data is private by default.
- Sharing is per-account, not per-transaction.
- Household pages show aggregates only, never other members' raw transactions or PII.
- PII is masked by default and intentionally revealed only on account PII surfaces.
- AI context preview shows what was sent and what was deliberately excluded.
- Household AI uses household aggregates; personal AI uses only the user's own book.
- Member lifecycle is testable: leave, grace, rejoin, purge, and last-owner protection.
- Mobile must preserve the same workflows, not only avoid runtime crashes.

## Manual Trigger Boundary

QA is intentionally not part of the autonomous development loop.

Development agents:

1. Implement the requested issue.
2. Run unit, lint, build, and targeted browser checks needed for confidence.
3. Open or update the PR.
4. Do not mark the work QA-certified.

QA agents:

1. Start only after a manual user request.
2. Work from `main`, preferably in a separate git worktree, unless the user explicitly asks to QA a PR branch or specific commit.
3. Start an isolated QA environment instead of using the development agent's running stack.
4. Confirm the commit range under review.
5. Verify behavior in headless Chrome and through API/database checks where relevant.
6. File or comment on GitHub issues with evidence.
7. Update the QA ledger with the last reviewed commit.
8. Do not fix the bugs found in the same QA session.

If the user asks the current agent to fix an issue found during QA, treat that as a role switch. Record the QA finding first, then stop calling the resulting work QA certification.

## Worktree And Environment Isolation

QA should not share a working tree, containers, ports, browser profile, or database volume with an active development agent.

Default checkout:

```sh
git worktree add ../offbook-qa main
```

If `../offbook-qa` already exists, update it non-destructively:

```sh
git -C ../offbook-qa checkout main
git -C ../offbook-qa pull --ff-only
```

Run QA from the QA worktree. Use a branch or detached commit only when the user explicitly asks to QA that target. Do not switch the developer's active worktree.

QA tooling requires a visible QA-mode signal. The default signal is running from a worktree whose directory name is `offbook-qa` or ends in `-qa`. If a scoped QA run must happen elsewhere, explicitly set:

```sh
export OFFBOOK_ROLE=qa
```

Helper scripts refuse to run without one of those signals, making accidental dev/QA role mixing visible.

Default isolated QA stack:

```sh
cp .env.qa.example .env.qa
# Edit .env.qa with a QA-only SESSION_SECRET and optional QA-only Plaid sandbox keys.
docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml up -d --build
```

QA service URLs:

- Frontend: `http://localhost:15173`
- Backend: `http://localhost:18000`
- Postgres host port: `15432`

The QA compose project name (`offbook-qa`) gives QA separate containers and Docker-managed identity. The QA compose override gives QA separate host ports and a dedicated `qa_postgres_data` named volume, so the QA database is isolated even if someone accidentally runs the QA stack from the main development worktree. A separate worktree is still required to avoid file and branch conflicts with the development agent.

The QA backend runs with `APP_ENV=qa` and defaults to the `offbook_qa` database. The init scripts create `offbook_qa` alongside `offbook_dev` and `offbook_test` on a fresh Postgres volume.

The backend container runs migrations on boot when `MIGRATIONS_PATH=/app/migrations` is set. On a cold QA volume, `docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml up -d --build` should leave the DB migrated and `/setup/admin` renderable.

Stop the QA stack when done:

```sh
docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml down
```

To reset only QA data, remove the QA volume after stopping the QA stack:

```sh
docker volume rm offbook-qa_qa_postgres_data
```

Use a dedicated Chrome profile for QA, for example under `/private/tmp/offbook-qa-chrome-profile`, so cookies and local storage do not bleed between development and QA.

## QA Environment And Credentials

The QA compose override loads backend environment from `.env.qa` and `.env.qa.local`, not the repo-root `.env`. This keeps QA Plaid sandbox credentials and `PLAID_TOKEN_KEY` separate from development. `.env.qa.local` is generated or edited locally and is gitignored.

`SESSION_SECRET` is intentionally blank in `.env.qa.example`. Set it to a QA-only value before booting the QA stack:

```sh
openssl rand -hex 32
```

Backend startup and `command make acceptance` refuse known placeholder values such as `replace-with-*`, `change-me`, and `changeme`.

QA persona emails are fixed. Passwords are derived from `QA_SECRET` by the acceptance fixture loader, so they are reproducible without committing plaintext credentials. If `QA_SECRET` is absent, `command make acceptance` writes a generated value to `.env.qa.local`; use that file for manual login during the same QA run.

Default personas:

| Persona | Email | Purpose |
| --- | --- | --- |
| Primary admin | `qa-admin@offbook.local` | First `/setup/admin` user, owner-level household flows, settings, invites |
| Contributor | `qa-contributor@offbook.local` | Household member with own accounts and share settings |
| Viewer | `qa-viewer@offbook.local` | View-only/member lifecycle and aggregate visibility checks |
| Solo user | `qa-solo@offbook.local` | Personal-only scope and no-household empty states |

The fixture loader provisions these personas idempotently:

```sh
node acceptance/fixtures/bootstrap.mjs
```

`command make acceptance` invokes the loader before running browser suites. If a test needs a one-off user, use a `qa+<suite>-<timestamp>@offbook.local` address and keep it inside the isolated QA stack.

## QA Ledger

Use GitHub Discussion #199 as the shared QA Ledger:

- `https://github.com/gregwym/offbook/discussions/199`

Do not record QA run history in committed repo files. A QA run should append a comment to Discussion #199 after filing or updating any issues.

Each full or scoped QA run gets a discussion comment:

```md
## QA Run — YYYY-MM-DD HH:MM PT — <scope>

- Reviewed commit: `<sha>`
- Compared from: `<previous-reviewed-sha>` or `none`
- Branch/target: `<branch | PR | commit>`
- Trigger: `<manual user request summary>`
- Environment: `<offbook-qa compose stack, browser, persona details>`
- Result: `<pass | issues filed | blocked>`
- Issues filed: `<none | #123>`
- Issues updated: `<none | #122>`
- Acceptance tests: `<not run | GitHub Actions run/check/artifact link | suites>`
- Notes: <short residual risk>

-- Codex QA
```

The most recent QA run comment in Discussion #199 is the source of truth for the last QAed commit. Fetch it with:

```sh
scripts/qa-last-reviewed.sh
```

Then compare that commit to the target under review:

```sh
git rev-parse HEAD
git log --oneline <last-reviewed-sha>..HEAD
git diff --stat <last-reviewed-sha>..HEAD
```

Use that delta to prioritize changed surfaces while still running baseline smoke checks.

## Issue Filing Standard

Every QA-filed bug must include the commit where it was found.

Required issue body fields:

```md
## Product Goal

## Found At
- Commit: `<sha>`
- Branch: `<branch>`
- QA run: `Discussion #199 comment <url>`

## Environment

## Reproduction

## Observed

## Expected

## Evidence

## Likely Root Cause

## Proposed Fix

## Regression Coverage

-- Codex QA
```

If a matching issue already exists, comment instead of creating a duplicate. The comment must still include `Found At: <sha>` if the failure is reproduced at a new commit.

When a QA-filed bug is fixed, re-test it before closing. The close comment must include:

```md
## QA Re-Verification

- Verified Fixed At: `<sha>`
- Verified By: `<QA ledger comment URL or GitHub Actions run URL>`
- Result: `<pass | still failing>`

-- Codex QA
```

Apply `qa-verified` on verified close. Use `qa-needs-verify` for a merged fix that still needs a QA re-test.

## Severity

- P0: privacy leak, auth bypass, cross-user data exposure, destructive action without confirmation.
- P1: crash, blank page, broken login/signup, impossible core workflow, incorrect financial total.
- P2: major responsive/layout failure, inaccessible primary action, misleading privacy or sharing state.
- P3: polish, copy mismatch, minor visual drift, weak empty state, nonblocking accessibility issue.

## Standard QA Run

1. Read the product contract files.
2. Check worktree and branch.
3. Run `scripts/qa-last-reviewed.sh` and compute the delta from the last reviewed commit.
4. Check open GitHub issues to avoid duplicates.
5. Start or confirm the isolated QA compose stack and backend health.
6. Log in with the test personas required for the run.
7. Run `command make acceptance`, or `command make qa-smoke` for a baseline-only run, then add changed-route checks as needed.
8. Run targeted workflow checks from the design contract.
9. Use API/database inspection to verify privacy and data-shape claims.
10. File or update issues with evidence and `Found At`.
11. Add or update acceptance tests when a core requirement is stable enough to automate.
12. Append a QA Ledger comment to Discussion #199, manually or with `scripts/qa-append-ledger.sh <markdown-file>`.

The canonical baseline route list lives in `acceptance/smoke/baseline.spec.ts`. It covers auth, personal, and household routes and asserts page load, no console/runtime errors, no 5xx responses, and no mobile horizontal overflow.

Browser checks:

- Desktop: `1280x900`.
- Mobile: `393x852` with iPhone Safari user agent.
- Use Playwright headless Chromium unless a bug specifically requires another browser.
- Use the isolated QA frontend URL, normally `http://localhost:15173`.
- Capture uncaught exceptions, console errors, blank page state, network 5xx responses, and horizontal overflow.
- For mobile, verify primary controls remain visible and usable.

## Acceptance Test Suites

Acceptance tests are separate from unit tests and from `make verify`. They exercise user-level product requirements through the running app and API. They start as optional, manually run checks and are not required for merging until the owner explicitly promotes them.

Location:

- Browser/API acceptance tests: `acceptance/`.
- Test fixtures and personas: `acceptance/fixtures/`.
- Reports and screenshots: ignored output under `acceptance/reports/`.

Command:

```sh
command make acceptance
```

Discover root-level QA commands with:

```sh
command make help
```

Run only the baseline suite:

```sh
command make qa-smoke
```

Run one suite or spec pattern:

```sh
command make qa-suite QA_SUITE=plaid
pnpm --dir acceptance exec playwright test plaid
OFFBOOK_ROLE=qa pnpm --dir acceptance exec playwright test smoke/baseline.spec.ts:19
```

Acceptance helpers that call `personaPassword()` load `.env.qa` and `.env.qa.local` as a side effect. Helpers that do not need persona passwords must call `loadQAEnv()` explicitly before reading `process.env`; the Plaid helper does this because Plaid credentials normally live in `.env.qa.local`.

The direct SQL persona bootstrap is only a fixture loader. It does not test `/setup/admin`, `/auth/signup`, `/auth/signup-with-invite`, or invite acceptance. Those first-boot product paths live in the opt-in cold-start suite, which is ignored by default because it resets QA data:

```sh
OFFBOOK_ROLE=qa QA_COLD_START=1 pnpm --dir acceptance exec playwright test auth/setup.cold-start.spec.ts
```

Default suites must not mutate the four shared personas. Write-heavy suites use `createThrowawayUser({ suite })` from `acceptance/fixtures/state.mjs` and clean up with `deleteThrowawayUsers(suite)`. If a run needs to scrub all mutable QA state and rebuild the shared personas, use:

```sh
OFFBOOK_ROLE=qa node acceptance/fixtures/bootstrap.mjs --reset
```

If a run needs true first-boot state without personas, use:

```sh
OFFBOOK_ROLE=qa node acceptance/fixtures/cold-start.mjs
```

Plaid sandbox acceptance must not drive the Plaid Link iframe. Use `acceptance/plaid/helper.mjs` to mint a public token through `/sandbox/public_token/create`, exchange it through `/api/v1/plaid/link/exchange`, then sync accounts and transactions through Offbook. The suite must assert accounts and transactions land for the signed-in QA user, and a second transaction sync inserts zero new rows. A smaller browser smoke should still assert `/connect` renders and the Plaid Link button mounts.

Initial suites to build:

1. Auth and setup: first admin setup, invite-only signup, signin/signout, unauthorized redirects.
2. Personal finance core: create account, PII masking/reveal, create/categorize transaction, dashboard totals.
3. Plaid sandbox smoke: public-token helper, exchange, sync accounts, sync transactions, no duplicate rows on re-sync.
4. Budgets and goals: create budget, spend calculation, over-budget warning, savings goal contribution.
5. Investments: empty portfolio, crypto precision, allocation summary, CSV import happy path.
6. Household privacy: invite member, private/balance-only/balance-and-transactions visibility, aggregate-only household UI/API.
7. Household lifecycle: leave, grace, rejoin, last-owner protection, purge preserving historical aggregates.
8. AI privacy: personal/household context exclusions and context-preview parity with provider-bound payload.
9. Responsive contract: mobile overflow, scope switcher, AI composer, and primary tap targets.

Each suite should write or link machine-readable run history. Today, manually link the local run output or note `not run` in the QA Ledger. When these suites are promoted into CI, GitHub Actions runs, checks, and artifacts become the durable acceptance-test run history. QA Ledger comments should then link the relevant Actions run/check/artifact instead of duplicating full logs in Discussion #199.

End every QA run with the commit reviewed, previous reviewed commit, changed surfaces tested, issues filed or updated, acceptance tests added or missing, and blockers or residual risk.
