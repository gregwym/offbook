# Offbook — Privacy-First Personal Finance

## Commands
- Backend dev server: `cd backend && make dev` (stops any prior instance first — kills by port to avoid collisions)
- Backend stop: `cd backend && make stop`
- Backend smoke (start + wait for /health): `cd backend && make smoke`
- Tests: `cd backend && make test`
- Lint: `cd backend && make lint`
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
- NEVER `git commit --amend`, `git reset --hard`, or `git push --force` (any variant). No exceptions.
- Fix mistakes with new commits, not history rewrites.

## Dev Dependencies
- Run all dev infrastructure (Postgres, Redis, queues, etc.) via Docker / `docker compose`, never native installs.
- Native installs only for language toolchains (Go, Node, etc.).
- If the Docker daemon isn't running, fix the daemon — don't bypass with a native install.

## Backlog Discipline
- Do NOT fix things outside the current issue
- Instead: `gh issue create --title "..." --body "..." --label backlog`
- Then return to current work

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
