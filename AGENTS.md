# Offbook — Privacy-First Personal Finance

## Commands
- Backend dev server: `cd backend && make dev` (stops any prior instance first — kills by port to avoid collisions)
- Backend stop: `cd backend && make stop`
- Backend smoke (start + wait for /health): `cd backend && make smoke`
- Tests: `cd backend && make test` (matches CI: `-race -p 1`). For fast iteration during dev, call `go test ./internal/<pkg>` directly.
- Verify (full CI mirror): `cd backend && make verify` (backend vet+test, frontend lint+build)
- Lint: `cd backend && make lint` (needs ≥4 GiB in the Docker VM — see `docs/dev/colima.md` if lint dies with `signal: killed during compile`)
- Format Go: `cd backend && docker run --rm -v "$PWD":/app -w /app golangci/golangci-lint:latest-alpine golangci-lint fmt ./...` (rewrites files in place; `golangci-lint run` then verifies)
- Frontend: `cd frontend && pnpm dev`
- Full stack: `docker compose up`
- Migrations: `cd backend && go run ./cmd/migrate {up|down|down-all|version|force <ver>}` (uses `.env`)
- Migration files: name as `migrations/{NNNNNN}_{slug}.{up|down}.sql`, 6-digit zero-padded sequence
- **After adding/changing a migration, run `cd backend && command make schema`** to regenerate [`docs/db/schema.md`](docs/db/schema.md) (a deterministic catalog report — the readable view of the current schema) and commit it in the same PR. The `Schema report up-to-date` CI job (and `make schema-check`, which runs inside `make verify`) fails if it drifts. Never hand-edit `schema.md`.

When running via Claude's Bash tool, prefix `make` with `command` (`command make dev`) — a zsh autoload stub in the shell snapshot shadows the binary. Interactive shells are unaffected.

## Working in Claude Sessions
- **All learnings go here, not in personal memory.** Project conventions, dev gotchas, workflow tips — anything worth saving across sessions — belongs in `AGENTS.md` (or a scoped rule under `.claude/rules/`, or `docs/`). `CLAUDE.md` is a symlink to `AGENTS.md` for cross-agent compatibility — edit `AGENTS.md`, never the symlink. Personal Claude memory is per-install and doesn't travel with the repo.
- Use `command make` (not raw `make`) — a zsh autoload stub in claude-code's shell snapshot shadows the binary in Claude's Bash tool. Interactive terminals are unaffected.
- Bash tool cwd persists across calls. Stay in the repo root for `git` (so it matches the allowlist's plain `git <verb>` patterns); `cd backend` only when running `make`/`go`/etc.
- Don't put a `cd` and a dependent command in the same tool turn as parallel calls — parallel Bash invocations have no defined order. Either chain them with `&&` (loses allowlist match) or send them as separate sequential calls (preferred).
- **Don't `&&`-chain or prepend `cd dir &&`.** The permission allowlist matches against the literal command string, so `cd backend && go test` doesn't match `Bash(go *)` even though both pieces are individually allowed. Run each command in its own Bash call; cwd persists.
- **Env vars belong in `.env`, never prepended.** `DATABASE_URL=... go run ...` defeats the allowlist for the same reason. The repo-root `.env` (gitignored; template at `.env.example`) is auto-loaded by `godotenv` (resolved from both `backend/` and the repo root) and by `docker compose`. Keep `.env.example` updated when adding new keys.
- For port-bound dev servers, always stop *by port* (`make stop` does this) — `go run` spawns a supervisor + child binary, so killing the saved PID isn't sufficient.

## Autonomous Workflow — Engineering-first, batched QA

The one active milestone is [Daily Driver v1](docs/product/DAILY-DRIVER-V1.md) (Goal [#423](https://github.com/gregwym/offbook/issues/423)). The source of truth is [autonomous-product-workflow.md](docs/dev/autonomous-product-workflow.md) and [agent-communication-protocol.md](docs/dev/agent-communication-protocol.md). Do not default to historical M13–M15 checkboxes.

- **Prefer implementation over prolonged planning.** Opus 5.5 is the preferred Engineering model when available; no weekly PR/token budget or arbitrarily low shipping cadence. Maintain a small queue of achievable acceptance-backed issues.
- One Engineering writer at a time; each Issue uses a canonical `agent/issue-N` branch claim, one active PR/branch at a time. Reuse open work; do not duplicate. Cloud and legacy Mac LaunchAgent must not concurrently write.
- Implement vertical slices including API/UI/regression tests, preserving decimal money, transfers, sync idempotence, reconciliation and PII invariants. For UI work, run desktop/mobile browser user journeys and fix obvious visual, friction, empty/loading/error/recovery and accessibility problems before merge. See [Experience Maturity Gate](docs/product/EXPERIENCE-MATURITY-GATE.md). Use synthetic/Plaid Sandbox data only in Cloud.
- **Every PR requires green *required CI* at the exact head SHA, and may then be squash-merged to `main` by Engineering without owner code review.** Don't bypass CI or knowingly merge a P0/P1 regression. This includes routine financial-domain code changes and staged, reversible migrations.
- **AI QA is not a per-PR merge gate.** An independent session performs financial QA **and product-experience/taste critique** on `main` in batches (default twice weekly or about eight merged PRs), records reviewed SHA range and findings on Goal #423, and opens Bug Issues. Fix P0/P1 findings before more unrelated features; reserve final release QA for the exact production candidate.
- Prioritize P0/P1 **and core-journey P2 UX/polish** defects over unrelated new features. CI-green = merged implementation, never automatically MATURE; only independent final QA may propose closing Goal #423 with current SHA and actual UX evidence.
- Stop for destructive/irreversible schema/data operations, real-bank/production secret access, paid external services, production rollout, or inability to meet existing protection rules — these need explicit owner authorization for the operation, **not code review**.
- Github CLI permissions and Cloud environment must be tested before enabling scheduled writers. `infra/cloud-routines/` are prompt templates, not running jobs.

## Git Discipline

- Auto-commit completed work without asking.
- **Protected `main`: work on a feature branch and merge by PR only.** Never push/commit directly to `main`.
- After required CI succeeds, squash-merge authorized development PRs without a separate QA session or human code review. If branch rules prevent merge, report actual blocker; do not fake success.
- NEVER `git commit --amend`, `git reset --hard`, or force-push. Fix mistakes with new commits.
- Dev Pi may auto-update with merged `main`; Prod Pi remains manually deployed. Never automate or access Production.

## Migration Safety
The pre-prod "wipe dev DBs and rebuild" era ends at M13 (#358). Once an instance holds real financial data, every schema change must be safe to take without risking that data.
- **Expand → migrate → contract.** Never rename or drop a column/table in the same migration that stops writing to it. Split any breaking change into stages that each work against the *currently deployed* code: (1) **expand** — add the new column/table/index, backfill, and start writing both old and new (deploy); (2) **migrate** — move reads to the new shape (deploy); (3) **contract** — drop the old column/table once nothing reads it (deploy). Each stage is independently deployable and independently reversible.
- **Destructive migrations need an explicit PR callout.** Any migration that drops or rewrites a column/table, or is otherwise not a pure additive expand, must say so in the PR description (a `**Destructive migration:**` line naming what it drops and why it's safe now). Reviewers gate on it.
- **Down-migrations are a dev/rollback tool, not a data-recovery tool.** A `down` reverses schema for local iteration and staged rollback; it does **not** restore data a contract step deleted. **Backups** are the recovery path (`make backup`/`make restore`, #357). `make deploy` takes an automatic backup *before* migrations run for exactly this reason.
- **Every migration ships a working `down`.** `make verify` (and CI) runs `make migrate-roundtrip` — a fresh `offbook_test` DB taken up → down → up, asserting clean completion and seed integrity. A missing or broken down file, or a seed that doesn't re-land, fails the build. Do not disable this to land a migration; fix the down file.

## Dev Dependencies
- Run all dev infrastructure (Postgres, Redis, queues, etc.) via Docker / `docker compose`, never native installs.
- Native installs only for language toolchains (Go, Node, etc.) — universal tooling, not project state.
- If the Docker daemon isn't running, fix the daemon (Colima / Podman / Docker Desktop) — don't bypass with a native install. When containerization is blocked, stop and ask.
- **Project-specific CLIs live in the repo, not the system.** Migration runners, code generators, and similar tools belong as `backend/cmd/<tool>/main.go` (or equivalent), invoked via `go run ./cmd/<tool>`. Example: `cmd/migrate` wraps `golang-migrate` — no system `migrate` binary needed. A teammate cloning the repo shouldn't have to `brew install` anything beyond the language toolchain.

## Environment Isolation
- **Every environment gets its own database.** Dev, test, prod (and any future staging) never share a DB. Sharing leaks fixtures between them — `make test` running against the dev DB has corrupted dev data in the past (test-fixture `categories` showing up in the user's category dropdown; see #183).
- DB selection routes through `APP_ENV`, resolved by `internal/config.ResolveDatabaseURL`. Defaults: `dev` → `offbook_dev`, `qa` → `offbook_qa`, `test` → `offbook_test`, `prod` → no default (refuses to start without explicit `DATABASE_URL`). Explicit `DATABASE_URL` always wins (docker-compose and prod inject one).
- When adding a new entry point (CLI, cron, worker), call `config.Load()` so it picks up the right DB automatically. Don't re-read `DATABASE_URL` from `os.Getenv` directly.
- `make dev` / `make smoke` set `APP_ENV=dev`. `make test` sets `APP_ENV=test` and points at `offbook_test`, migrating it first.
- The isolated QA compose stack sets `APP_ENV=qa` and points at `offbook_qa`.
- One-shot cleanup of a polluted pre-#183 DB: `cd backend && go run ./cmd/db-clean-fixtures --apply` (dry-run by default; refuses to run against `offbook_test`).
- "Just use a `TEST_DATABASE_URL` env var" is not enough — the structural fix is config-layer isolation across all envs, not a Makefile flag.

## Plaid Sandbox
- Set `PLAID_CLIENT_ID`, `PLAID_SECRET`, `PLAID_ENV=sandbox`, and `PLAID_TOKEN_KEY` (32-byte hex, `openssl rand -hex 32`) in `.env`.
- Frontend entry point is `/connect` ("Connect Bank" in the personal sidebar). Click → Plaid Link opens → pick any sandbox institution (e.g. Chase) → sign in with `user_good` / `pass_good`. The page chains exchange → sync-accounts → sync-transactions, then navigates to `/accounts`.
- Settings → "Linked Institutions" lists each `plaid_items` row and exposes a per-item Disconnect (soft-delete; accounts and historical transactions remain).
- All Plaid surfaces are personal-scope only — sharing into a household is per-account via `account_shares`, not per-item.
- **Extending `plaid_category_map` for new PFC values:** the map lives in SQL (migration 000005 + follow-ups like 000028), never Go literals — reviewable in git, no release needed to fix a bad mapping. When real (non-sandbox) traffic surfaces an uncategorized transaction, find its `personal_finance_category.{primary,detailed}` (log it, or check via the DB), look up the closest local category slug (`categories.slug`, seeded in migration 000002), and add a new data-only migration in the 000005 pattern: `INSERT INTO plaid_category_map (...) SELECT ... FROM (VALUES (...)) JOIN categories ... ON CONFLICT DO NOTHING`. Ship a matching `.down.sql` that deletes only the rows the `.up.sql` added — never `DROP TABLE`. Detailed values are the Plaid wire format: primary repeated as a prefix (`FOOD_AND_DRINK_GROCERIES`, not `GROCERIES`) — see 000012.

## Tailscale Deployment
- Multi-instance deployment on one host runs **one Compose project per instance, each with its own Tailscale sidecar** — see [ADR-0016](docs/ADR/0016-tailscale-per-instance-deployment.md).
- **Deploy is convention-driven (`make deploy`).** The compose project name + overlay list come from `FLAVOR` (default `dev` → project `offbook`, base + tailscale sidecar; `FLAVOR=prod` → `offbook-prod`, base + prod + sidecar, `.env.prod`). The env file holds **secrets only**; `make deploy` creates it and generates `SESSION_SECRET` on first boot (never rotates an existing one). Don't reintroduce `OFFBOOK_PROJECT`/`OFFBOOK_COMPOSE_FILES` as *required* config — they remain an env-file escape hatch only.
- **Tailscale identity is bootstrap-only.** `TS_AUTHKEY` and `TS_HOSTNAME` are read only when the sidecar is first registered; pass both once on the first-boot command line (`make deploy TS_AUTHKEY=... TS_HOSTNAME=...`), never store them. Later deploys recreate only backend+frontend (`--no-deps`) and need neither.
- Sidecar override is `docker-compose.tailscale.yml`. Instance-agnostic; composes with any base stack. Walkthrough in `infra/tailscale/README.md`.
- **Auto-deploy + teardown have no-edit make targets:** `make auto-deploy-install` / `auto-deploy-uninstall` (user-level systemd timer, per `FLAVOR`), `make down` (stop), `make teardown` (drop volumes). See `infra/auto-deploy/README.md`. Don't hand-edit the systemd units — `install.sh` renders them from the templated `offbook-deploy@.{service,timer}`.
- Do not multiplex multiple instances behind a single Tailscale node (path-based or port-based). One node per instance = one MagicDNS hostname per instance, which is the only configuration the React app and cookie scoping don't have to know about.

## Backlog Discipline
- Do NOT fix things outside the current issue
- Instead: `gh issue create --title "..." --body "..." --label backlog`
- Then return to current work

## Frontend↔Backend Contract Discipline
- **Removing a backend route requires removing or migrating every frontend caller in the same PR.** Bugs #266 and #268 both shipped because a route was deleted in isolation (M10a/#240 dropped the `/investments` wiring per ADR-0013) and the frontend kept calling it. Before deleting a handler or its Register call, grep `frontend/src/api/*.ts` for the URL string. If the replacement lands in a follow-on PR (e.g. ADR-0013 → M10b #238), either redirect the caller to the new route, render an explicit empty state, or temporarily wrap the call in `.catch(...)` so the page degrades — never leave a frontend caller pointed at a 404.
- `make contract-check` (and the `Contract check` CI job) enforces this mechanically: every `apiClient.<method>('<path>')` in `frontend/src/api/*.ts` must map to a registered backend route. The check is sub-second and runs first inside `make verify`. If it fails, fix the contract — never silence the check.
- The full layered test-gap plan that produced this rule lives in epic #270.

## Scopes & Households (M2.5+)
- Every domain row (account, transaction, budget, savings goal, investment, AI thread) carries `user_id NOT NULL`. Always derive `user_id` from the session, never trust it in the request body.
- Two scopes per user: **personal** (own book) and **household** (shared). Mutually exclusive route lists. Default at login: `household` if member of one, else `personal`. Persists per user in `users.last_scope`.
- A user belongs to **at most one household**. Many households can coexist on the same instance without seeing each other.
- Account-level visibility, 3 states: `private` (default; no `account_shares` row) | `balance_only` | `balance_and_txns`. Set per account per household. Sharing an account shares all its transactions; there's no per-transaction toggle.
- **Cross-user reads only via the aggregator** (`internal/service/household/aggregator.go`). Handlers under `/h/...` call the aggregator, not repositories. The aggregator never imports `pii_repo`, never returns raw transaction rows, and excludes `private` accounts and in-grace members from live aggregates. Live = current period; historical = preserved across grace expiry and purge.
- **Auth modes set at first boot:** `local_multi_tenant` (anyone signs up) or `invite_only` (admin issues tokens). Default = `invite_only`. The first user to hit `/setup/admin` becomes admin and picks the mode.
- **Lifecycle:** voluntary leave is self-service (no owner approval). Last/only owner leaving → `409 LAST_OWNER`. Rejoin within `grace_period_days` (default 30, owner-configurable) auto-resumes prior shares and shared-thread visibility. After grace expires, links are purged but past contributions remain in historical aggregates.
- See [ADR-0006](docs/ADR/0006-multi-tenant-model.md), [ADR-0007](docs/ADR/0007-member-lifecycle.md), [ADR-0008](docs/ADR/0008-household-aggregation-layer.md), and `docs/designs/App Hierarchy v4.html`.

## Key References
- Architecture: @docs/ARCHITECTURE.md
- Decisions: @docs/ADR/
- Roadmap: @docs/ROADMAP.md

## Scoped Rules
Apply these when touching matching files:
- @.claude/rules/go-backend.md
- @.claude/rules/database.md
- @.claude/rules/frontend.md
- @.claude/rules/testing.md
