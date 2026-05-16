# Offbook — Privacy-First Personal Finance

## Commands
- Backend: `cd backend && go run cmd/server/main.go`
- Tests: `cd backend && go test ./...`
- Lint: `cd backend && golangci-lint run`
- Frontend: `cd frontend && pnpm dev`
- Full stack: `docker compose up`
- Migrations up: `cd backend && migrate -path migrations -database "$DATABASE_URL" up`
- Migrations new: `migrate create -ext sql -dir backend/migrations -seq <name>`

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
