# Offbook QA Workflow

Offbook QA is a manually triggered role. The same agent session should not both implement a change and certify it as QA. A developer agent may run focused checks while building, but autonomous QA starts only when the user explicitly asks for it, for example: "be QA", "run QA", "QA issue #123", or "QA since the last reviewed commit."

QA work is evidence gathering, issue filing, and acceptance-test development. It must not make product code changes unless the user explicitly switches the session from QA to development. QA should normally use headless Chrome for browser verification.

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

## Worktree and Environment Isolation

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

Default isolated QA stack:

```sh
docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml up -d --build
```

QA service URLs:

- Frontend: `http://localhost:15173`
- Backend: `http://localhost:18000`
- Postgres host port: `15432`

The QA compose project name (`offbook-qa`) gives QA separate containers and Docker-managed identity. The QA compose override gives QA separate host ports and a dedicated `qa_postgres_data` named volume, so the QA database is isolated even if someone accidentally runs the QA stack from the main development worktree. A separate worktree is still required to avoid file and branch conflicts with the development agent.

Stop the QA stack when done:

```sh
docker compose -p offbook-qa -f docker-compose.yml -f docker-compose.qa.yml down
```

To reset only QA data, remove the QA volume after stopping the QA stack:

```sh
docker volume rm offbook-qa_qa_postgres_data
```

Use a dedicated Chrome profile for QA, for example under `/private/tmp/offbook-qa-chrome-profile`, so cookies and local storage do not bleed between development and QA.

## QA Credentials

QA credentials are fixed test personas for the isolated QA stack only. They are not secrets and must not be used in development, production, or any shared real-data environment.

Default personas:

| Persona | Email | Password | Purpose |
| --- | --- | --- | --- |
| Primary admin | `qa-admin@offbook.local` | `qa-admin-password-2026!` | First `/setup/admin` user, owner-level household flows, settings, invites |
| Contributor | `qa-contributor@offbook.local` | `qa-contributor-password-2026!` | Household member with own accounts and share settings |
| Viewer | `qa-viewer@offbook.local` | `qa-viewer-password-2026!` | View-only/member lifecycle and aggregate visibility checks |
| Solo user | `qa-solo@offbook.local` | `qa-solo-password-2026!` | Personal-only scope, no-household empty states |

On a fresh QA database, create the primary admin through `/setup/admin` with `invite_only` mode unless the test specifically covers local multi-tenant signup. Create the remaining personas through invite flows so QA exercises the product path.

Acceptance tests should use these credentials by default and may recreate them after resetting the QA volume. If a test needs a one-off user, use a `qa+<suite>-<timestamp>@offbook.local` address and keep it inside the isolated QA stack.

## QA Ledger

Use GitHub Discussion #199 as the shared QA Ledger:

- `https://github.com/gregwym/offbook/discussions/199`

Do not record QA run history in committed repo files. A QA run should append a comment to Discussion #199 after filing or updating any issues. This keeps QA bookkeeping shared across agents without creating PRs only for operational state.

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

The most recent QA run comment in Discussion #199 is the source of truth for the last QAed commit. At the start of a QA run, compare that commit to the target under review:

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

## Severity

- P0: privacy leak, auth bypass, cross-user data exposure, destructive action without confirmation.
- P1: crash, blank page, broken login/signup, impossible core workflow, incorrect financial total.
- P2: major responsive/layout failure, inaccessible primary action, misleading privacy or sharing state.
- P3: polish, copy mismatch, minor visual drift, weak empty state, nonblocking accessibility issue.

## Standard QA Run

1. Read the product contract files.
2. Check worktree and branch.
3. Read the latest QA Ledger comment in Discussion #199 and compute the delta from the last reviewed commit.
4. Check open GitHub issues to avoid duplicates.
5. Start or confirm the isolated QA compose stack and backend health.
6. Log in with the test personas required for the run.
7. Run browser smoke over changed routes and baseline critical routes.
8. Run targeted workflow checks from the design contract.
9. Use API/database inspection to verify privacy and data-shape claims.
10. File or update issues with evidence and `Found At`.
11. Add or update acceptance tests when a core requirement is stable enough to automate.
12. Append a QA Ledger comment to Discussion #199.

Critical baseline routes:

- Personal: `/dashboard`, `/accounts`, `/transactions`, `/rules`, `/budgets`, `/savings-goals`, `/investments`, `/import`, `/ai`, `/settings`.
- Household: `/h/dashboard`, `/h/budgets`, `/h/goals`, `/h/members`, `/h/ai`, `/h/settings`.
- Auth: `/setup/admin`, `/signin`, `/signup`.

Browser checks:

- Desktop: `1280x900`.
- Mobile: `393x852` with iPhone Safari user agent.
- Use headless Chrome unless a bug specifically requires another browser.
- Use the isolated QA frontend URL, normally `http://localhost:15173`.
- Capture uncaught exceptions, console errors, blank page state, network 5xx responses, and horizontal overflow.
- For mobile, verify primary controls remain visible and usable.

## Acceptance Test Suites

Acceptance tests are separate from unit tests and from `make verify`. They should exercise user-level product requirements through the running app and API. They start as optional, manually run checks and are not required for merging until the owner explicitly promotes them.

Location:

- Browser/API acceptance tests: `acceptance/`.
- Test fixtures and personas: `acceptance/fixtures/`.
- Reports and screenshots: ignored output under `acceptance/reports/`.

Expected command shape once implemented:

```sh
command make acceptance
```

Acceptance tests should target the isolated QA stack by default, not the developer stack. If they need to start services themselves, they should use the `offbook-qa` compose project and `docker-compose.qa.yml` ports.

Initial suites to build:

1. Auth and setup
   - first admin setup
   - invite-only signup
   - signin/signout
   - unauthorized redirects

2. Personal finance core
   - create account
   - PII masked by default and revealable only from account PII surface
   - create transaction
   - categorize transaction
   - dashboard totals update

3. Plaid sandbox smoke
   - connect sandbox institution
   - sync accounts
   - sync transactions
   - re-sync does not duplicate

4. Budgets and goals
   - create budget
   - budget spend calculation
   - over-budget warning
   - create savings goal and contribution

5. Investments
   - empty portfolio renders
   - crypto precision is preserved
   - allocation summary renders
   - CSV import happy path

6. Household privacy
   - create household
   - invite member
   - private account excluded
   - balance-only account contributes balance but not transaction category detail
   - balance-and-transactions account contributes allowed aggregates
   - no raw other-member transactions appear in household UI or API responses

7. Household lifecycle
   - member leaves
   - leaving member is excluded from live aggregates during grace
   - rejoin auto-resumes preserved links
   - last owner cannot leave without transfer
   - purge removes expired links while preserving historical aggregates

8. AI privacy
   - personal AI context excludes PII
   - household AI context excludes PII, private accounts, individual balances, raw other-member transactions, and other members' private chats
   - context preview matches the API payload sent to the model provider boundary

9. Responsive contract
   - no route-level horizontal overflow on mobile except intentional local table scrollers
   - scope switcher usable on mobile
   - AI composer visible on mobile
   - primary actions meet mobile tap-target expectations

Acceptance test run history:

- Manual QA narrative belongs in Discussion #199.
- Automated acceptance test history should eventually live in GitHub Actions runs, non-required checks, logs, screenshots, and uploaded artifacts.
- When acceptance suites are introduced, publish a GitHub Actions run or check result against the reviewed commit and link it from the Discussion #199 QA run comment.
- Keep acceptance checks non-required until the owner explicitly promotes a stable subset.

Promotion rule:

- Optional at first: failures file bugs but do not block merges.
- Required later only by explicit decision, after the suite is stable and fast enough for CI.
- When promoted, document the required subset in `AGENTS.md` and CI config in the same PR.

## QA Output

End every QA run with:

- commit reviewed
- previous reviewed commit
- changed surfaces tested
- issues filed or updated
- acceptance tests added or missing
- blockers or residual risk
