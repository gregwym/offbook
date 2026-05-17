---
paths:
  - "backend/**/*.go"
---

# Go Backend Rules
- Handlers: parse request → call service → return JSON. No business logic in handlers.
- Response envelope: every handler wraps. Success-list → `{"data": [...], "total": N}`; success-single → `{"data": {...}}`; error → `{"error": "...", "code": "..."}`. No bare-object responses, including infra/health.
- Services: receive repository interfaces as constructor args. Never instantiate DB directly.
- Repositories: GORM calls only. Return domain models, not GORM-specific types.
- Money: shopspring/decimal everywhere. Never float64 for monetary values.
- Errors: services return domain errors (ErrAccountNotFound, ErrDuplicateTransaction).
  Handlers map domain errors to HTTP status codes.
- Naming: receiver names are short (1-2 chars). Interface names don't start with I.
- Auth: handlers derive `user_id` from the session, never from request body/query. Repositories that read user-owned tables take `user_id` as a parameter and filter on it.
- Cross-user reads: only `internal/service/household/aggregator.go` may read across user_ids, and only within a single household via opt-in shares. New household-facing handlers under `/h/...` call the aggregator, never repositories.
- The aggregator package must NEVER import `pii_repo` — same hard rule as `service/ai/`.
