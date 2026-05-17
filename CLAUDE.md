# Offbook — Privacy-First Personal Finance

## Commands
- Backend dev server: `cd backend && make dev` (stops any prior instance first — kills by port to avoid collisions)
- Backend stop: `cd backend && make stop`
- Backend smoke (start + wait for /health): `cd backend && make smoke`
- Tests: `cd backend && make test`
- Lint: `cd backend && make lint`
- Format Go: `cd backend && docker run --rm -v "$PWD":/app -w /app golangci/golangci-lint:latest-alpine golangci-lint fmt ./...` (rewrites files in place; `golangci-lint run` then verifies)
- Frontend: `cd frontend && pnpm dev`
- Full stack: `docker compose up`
- Migrations: `cd backend && go run ./cmd/migrate {up|down|down-all|version|force <ver>}` (uses `.env`)
- Migration files: name as `migrations/{NNNNNN}_{slug}.{up|down}.sql`, 6-digit zero-padded sequence

When running via Claude's Bash tool, prefix `make` with `command` (`command make dev`) — a zsh autoload stub in the shell snapshot shadows the binary. Interactive shells are unaffected.

## Working in Claude Sessions
- **All learnings go here, not in personal memory.** Project conventions, dev gotchas, workflow tips — anything worth saving across sessions — belongs in CLAUDE.md (or a scoped rule under `.claude/rules/`, or `docs/`). Personal Claude memory is per-install and doesn't travel with the repo.
- Use `command make` (not raw `make`) — a zsh autoload stub in claude-code's shell snapshot shadows the binary in Claude's Bash tool. Interactive terminals are unaffected.
- Bash tool cwd persists across calls. Stay in the repo root for `git` (so it matches the allowlist's plain `git <verb>` patterns); `cd backend` only when running `make`/`go`/etc.
- Don't put a `cd` and a dependent command in the same tool turn as parallel calls — parallel Bash invocations have no defined order. Either chain them with `&&` (loses allowlist match) or send them as separate sequential calls (preferred).
- **Don't `&&`-chain or prepend `cd dir &&`.** The permission allowlist matches against the literal command string, so `cd backend && go test` doesn't match `Bash(go *)` even though both pieces are individually allowed. Run each command in its own Bash call; cwd persists.
- **Env vars belong in `.env`, never prepended.** `DATABASE_URL=... go run ...` defeats the allowlist for the same reason. The repo-root `.env` (gitignored; template at `.env.example`) is auto-loaded by `godotenv` (resolved from both `backend/` and the repo root) and by `docker compose`. Keep `.env.example` updated when adding new keys.
- For port-bound dev servers, always stop *by port* (`make stop` does this) — `go run` spawns a supervisor + child binary, so killing the saved PID isn't sufficient.

## Autonomous Workflow
1. Read @docs/ROADMAP.md → find current milestone
2. `gh issue list --state open --milestone "<current milestone>"`
3. Pick top unstarted issue
4. `git checkout -b feature/{issue-number}-{slug}`
5. Implement to acceptance criteria in the issue
6. `cd backend && go test ./...` → fix any failures
7. Commit, push, `gh pr create --body "Closes #{issue-number}"`
8. Move to next issue

## Git Discipline
- Auto-commit completed work without asking.
- On `main`: push freely to preserve work. On feature branches: push when opening/updating a PR.
- **PRs may be merged without external review** — owner has pre-authorized self-merge for autonomous workflow. Prefer `gh pr merge --squash --delete-branch` once CI (if any) is green and the branch is mergeable.
- NEVER `git commit --amend`, `git reset --hard`, or `git push --force` (any variant). No exceptions.
- Fix mistakes with new commits, not history rewrites.

## Dev Dependencies
- Run all dev infrastructure (Postgres, Redis, queues, etc.) via Docker / `docker compose`, never native installs.
- Native installs only for language toolchains (Go, Node, etc.) — universal tooling, not project state.
- If the Docker daemon isn't running, fix the daemon (Colima / Podman / Docker Desktop) — don't bypass with a native install. When containerization is blocked, stop and ask.
- **Project-specific CLIs live in the repo, not the system.** Migration runners, code generators, and similar tools belong as `backend/cmd/<tool>/main.go` (or equivalent), invoked via `go run ./cmd/<tool>`. Example: `cmd/migrate` wraps `golang-migrate` — no system `migrate` binary needed. A teammate cloning the repo shouldn't have to `brew install` anything beyond the language toolchain.

## Backlog Discipline
- Do NOT fix things outside the current issue
- Instead: `gh issue create --title "..." --body "..." --label backlog`
- Then return to current work

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
