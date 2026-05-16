# Offbook — Privacy-First Personal Finance

## Commands
- Backend: `cd backend && go run cmd/server/main.go`
- Tests: `cd backend && go test ./...`
- Lint: `cd backend && docker run --rm -v "$PWD":/app -w /app golangci/golangci-lint:latest-alpine golangci-lint run ./...`
- Frontend: `cd frontend && pnpm dev`
- Full stack: `docker compose up`
- Migrations: `cd backend && go run ./cmd/migrate {up|down|down-all|version|force <ver>}` (uses `.env`)
- Migration files: name as `migrations/{NNNNNN}_{slug}.{up|down}.sql`, 6-digit zero-padded sequence

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
