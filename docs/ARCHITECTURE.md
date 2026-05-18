# Offbook Architecture

Privacy-first personal finance app. All data stays local. Self-hostable via Docker.

## Guiding Principles

1. **Privacy by architecture** — PII lives in a dedicated `pii_store` table, isolated from all other queries. The AI service layer has no access to PII.
2. **Precision over convenience** — All monetary values use PostgreSQL `NUMERIC(30,18)` and Go `shopspring/decimal`. No floats, no integer cents. Required for crypto (18 decimal places).
3. **Local-first** — Runs on localhost. No external services except Plaid (opt-in) and AI providers (opt-in, data-minimized).
4. **Soft deletes everywhere** — Financial data is never hard-deleted. `deleted_at TIMESTAMPTZ` on all domain tables.
5. **Multi-tenant by structure** — Every domain row is owned by a `user_id`. Cross-user reads happen only through the household aggregator (see [ADR-0008](ADR/0008-household-aggregation-layer.md)), which enforces opt-in account sharing and excludes PII by construction.

## Tech Stack

| Layer | Choice | Notes |
|---|---|---|
| Backend | Go + Gin | Handlers → Services → Repositories |
| Database | PostgreSQL | NUMERIC type for exact decimal arithmetic |
| Migrations | golang-migrate | Sequential SQL files in `backend/migrations/` |
| Frontend | React + Vite + TypeScript | Zustand for state, Recharts for charts |
| Containerization | Docker Compose | backend + frontend + postgres |
| AI | Pluggable (Claude API / Ollama) | Switchable at runtime |

## Directory Layout

```
backend/
├── cmd/server/main.go            # Entry point
├── internal/
│   ├── config/config.go          # Env-based config struct (godotenv + struct)
│   ├── db/db.go                  # GORM connection (schema is owned by golang-migrate, NOT AutoMigrate)
│   ├── model/                    # GORM models — one file per domain entity. Models mirror migrations; never use AutoMigrate.
│   ├── handler/                  # Gin handlers — thin, parse request → call service → respond
│   ├── service/                  # Business logic — receives repo interfaces
│   │   ├── ai/                   # AI provider protocol, context builder, service
│   │   ├── auth/                 # Session middleware, signup, first-boot wizard
│   │   ├── household/            # Aggregator — ONLY cross-user reader (ADR-0008)
│   │   └── pii_service.go        # ONLY service with pii_repo access
│   ├── repository/               # DB access — interfaces + GORM implementations
│   │   └── pii_repo.go           # Only injected into pii_service
│   └── router/router.go          # Route registration, middleware
├── cmd/
│   ├── server/                   # Entry point
│   ├── migrate/                  # golang-migrate wrapper
│   └── household-purge/          # Grace-period purge runner (deferred — see ADR-0007)
├── migrations/                   # golang-migrate SQL files (000001_init.up.sql, etc.)
├── go.mod
└── Dockerfile

frontend/
├── src/
│   ├── api/                      # Typed API client layer (one file per domain)
│   ├── types/                    # TypeScript interfaces mirroring backend schemas
│   ├── store/                    # Zustand stores
│   ├── hooks/                    # Custom React hooks
│   ├── pages/                    # Route-level components
│   └── components/               # Reusable UI components
├── package.json
└── Dockerfile
```

## Data Flow

### Transaction Ingestion (Plaid)
```
Plaid API → plaid_service.go → transaction_service.go → transaction_repo.go → DB
                                     ↓
                              PII fields → pii_service.go → pii_repo.go → pii_store table
```

### Transaction Ingestion (CSV/PDF — future)
```
Upload → ingestion handler → csv_ingester / pdf_ingester → transaction_service.go → DB
```

### AI Query
```
User message → ai_service.go → context_builder.go (queries DB, EXCLUDES pii_store)
                                     ↓
                              provider.go → Claude API or Ollama
                                     ↓
                              Response → stored in ai_messages with context_snapshot
```

**Critical constraint:** `context_builder.go` and `ai_service.go` must NEVER receive `pii_repo` as a dependency. This is the architectural enforcement of PII isolation.

### Household Aggregation
```
Household surface → aggregator.go (service/household)
                         ↓
                    Reads: tx_repo, acct_repo, share_repo, member_repo,
                           sb_repo, sg_repo, ai_thread_repo
                         ↓
                    Filters: visibility ≥ balance_only,
                             member.left_at IS NULL (live aggregates)
                         ↓
                    Returns: sums, counts, percentages — never raw txns
```

**Critical constraint:** `service/household/` must NEVER import `pii_repo`. Aggregator outputs contain no PII and no raw transaction rows. See [ADR-0008](ADR/0008-household-aggregation-layer.md).

## Multi-Tenant Model

One instance hosts many users; each user belongs to at most one household. Two scopes — personal and household — drive mutually exclusive route lists in the sidebar. Account-level visibility (3 levels) gates what household members see. Member lifecycle is voluntary-leave + grace-period rejoin. Full rationale in [ADR-0006](ADR/0006-multi-tenant-model.md) and [ADR-0007](ADR/0007-member-lifecycle.md).

**Tables added in M2.5:**
- `users` — auth principal. `email`, `password_hash`, `is_admin`, `last_scope`, `default_scope`
- `sessions` — cookie-backed; rotated on signin
- `instance_config` — singleton row; `signup_mode` ∈ {`local_multi_tenant`, `invite_only`}
- `households` — `name`, `owner_id`, `grace_period_days` (default 30)
- `household_members` — `household_id`, `user_id`, `role` ∈ {`owner`, `contributor`, `view_only`}, `joined_at`, `left_at`, `purged_at`. Soft-delete-safe unique on `(household_id, user_id) WHERE purged_at IS NULL`.
- `household_invites` — token-based invites for `invite_only` mode
- `account_shares` — `account_id`, `household_id`, `visibility` ∈ {`private`, `balance_only`, `balance_and_txns`}. Absence of row = `private`. Unique on `(account_id, household_id) WHERE deleted_at IS NULL`.
- `shared_budgets`, `shared_goals` — household-scoped versions of personal counterparts (tables only in M2.5; CRUD/UI later)

**Tables augmented in M2.5:**
- `accounts`, `transactions`, `budgets`, `savings_goals`, `investments` — add `user_id BIGINT NOT NULL REFERENCES users(id)`. All M2-era endpoints derive `user_id` from session.
- `ai_conversations` is renamed to `ai_threads` and gains `user_id`, `household_id NULL`, `shared_with_household BOOLEAN DEFAULT false`. `ai_messages.conversation_id` becomes `thread_id`.

**Scope state:** `users.last_scope` ∈ {`personal`, `household`}. `GET /me/scope` returns active + available scopes. `PATCH /me/scope` persists the switch. Defaults to `household` if the user is a member of one, else `personal`.

## PII Isolation

### Table: `pii_store`
```sql
id            BIGSERIAL PRIMARY KEY
entity_type   TEXT NOT NULL      -- 'account' | 'transaction' | 'institution'
entity_id     BIGINT NOT NULL
field_name    TEXT NOT NULL      -- 'holder_name' | 'account_number' | 'routing_number' | 'address'
value         TEXT NOT NULL
created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
UNIQUE (entity_type, entity_id, field_name)
```

### Access rules
- `pii_repo.go` — the ONLY repository that reads/writes `pii_store`
- `pii_service.go` — the ONLY service that receives `pii_repo`
- All other services and the AI layer CANNOT access PII
- The household aggregator (`service/household/`) CANNOT access PII
- Frontend accesses PII via explicit `/accounts/:id/pii` endpoint — deliberate, auditable
- Ownership of a `pii_store` row is transitive through `entity_id`: PII belongs to whichever user owns the referenced account/transaction. `pii_service.go` must check the entity's `user_id` matches the session before returning the row.
- **Orphan-cleanup policy:** soft-delete on an account/transaction **preserves** its PII rows ([ADR-0009](ADR/0009-pii-orphan-cleanup-policy.md)). Soft-deleted entities are unreachable through the API (the transitive ownership check rejects them), but their PII survives until a hard purge. Hard purge MUST delete `pii_store` rows in the same transaction as the entity hard-delete.

### What goes in pii_store vs main tables

| Data | Where | Why |
|---|---|---|
| Account holder name | `pii_store` | PII |
| Full account number | `pii_store` | PII |
| Routing number | `pii_store` | PII |
| Physical address | `pii_store` | PII |
| Account label ("Chase Checking") | `accounts.name` | User-chosen, not PII |
| Last 4 digits | `accounts.last_four` | Display convenience, not identifying |
| Institution slug ("chase") | `accounts.institution_slug` | Not PII |

## Database Schema

### Money columns
All monetary values: `NUMERIC(30, 18)`. In Go: `github.com/shopspring/decimal`.

### IDs
All primary keys: `BIGSERIAL` / `BIGINT`. Future-proof for high-volume tables (transactions, ai_messages). `pii_store.entity_id` is `BIGINT` — no FK by design, since PII isolation precludes a join target.

### Soft-delete-safe uniqueness
Any `UNIQUE` constraint on a soft-deletable table must be a partial index excluding deleted rows. Example:
```sql
CREATE UNIQUE INDEX uq_transactions_external
  ON transactions (account_id, external_id)
  WHERE deleted_at IS NULL;
```
Otherwise re-importing a previously-deleted transaction fails.

### Core tables
- **accounts** — `id, name, institution_slug, account_type, currency, balance, last_four, plaid_account_id, plaid_item_id, is_active, created_at, updated_at, deleted_at`
- **transactions** — `id, account_id, category_id, amount, currency, description, description_clean, merchant_name, transaction_date, posted_date, source, external_id, plaid_transaction_id, categorization_method, is_transfer, transfer_pair_id, notes, created_at, updated_at, deleted_at`
  - `source`: `'plaid' | 'csv' | 'pdf' | 'manual'`
  - `categorization_method`: `'manual'` (user picked) | `'plaid_default'` (auto-assigned from `plaid_category_map` at Plaid sync). Null when the row is uncategorized. Manual always wins on merge — see `service/plaid/transaction_mapping.go::MergePlaidUpdate`.
  - Partial unique index on `(account_id, external_id) WHERE deleted_at IS NULL` for deduplication (see Soft-delete-safe uniqueness above)
  - Partial unique index on `(user_id, plaid_transaction_id) WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL` (migration 000004) — user-scoped Plaid dedup
- **categories** — hierarchical via `parent_id`, seeded with ~20 system categories
- **categorization_rules** — `pattern, category_id, match_type ('contains'|'regex'|'exact'), priority`
- **plaid_category_map** — `plaid_primary, plaid_detailed, category_id`. Static lookup table (migration 000005) mapping the Plaid `personal_finance_category` taxonomy to local categories. Editable by SQL migration only — keeps the mapping reviewable in git. Loaded once into a `CategoryMapper` at service construction.
- **budgets** — `category_id, period ('monthly'|'weekly'|'annual'), amount, rollover, is_active`
- **savings_goals** — `name, target_amount, current_amount, target_date, account_id`
- **investments** — append-only snapshots: `account_id, ticker, name, asset_class, quantity, cost_basis, market_value, snapshot_date, source`
- **ai_threads** — `id, user_id, household_id NULL, shared_with_household BOOL DEFAULT false, title, created_at, updated_at, deleted_at` (renamed from `ai_conversations` in M2.5)
- **ai_messages** — `thread_id, role, content, context_snapshot (JSONB), provider, model_name, created_at`
- **pii_store** — see PII Isolation section
- **users / sessions / instance_config / households / household_members / household_invites / account_shares / shared_budgets / shared_goals** — see Multi-Tenant Model section

## API Conventions

### Base path
All routes under `/api/v1/`.

### Response format
**Every** JSON response from the backend wraps. No bare-object responses, including infra/health endpoints — this keeps the frontend `ApiList<T>` / `ApiItem<T>` / `ApiError` typing uniform and avoids per-route special cases.

```json
// Success (list)
{"data": [...], "total": 42}

// Success (single)
{"data": {...}}

// Error
{"error": "Human-readable message", "code": "MACHINE_READABLE_CODE"}
```

`/api/v1/health` returns `{"data": {"status": "ok"}}` on success and `{"data": {"status": "down", "db": "..."}, "error": "...", "code": "DB_UNAVAILABLE"}` on failure. Liveness probes that only care about HTTP status (e.g. `curl -sf`) keep working; consumers that parse the body get the envelope.

### Pagination
Query params: `?limit=50&offset=0`. Default limit: 50, max: 200.

### Dates
RFC3339 in JSON. `DATE` columns as `"2024-01-15"`. `TIMESTAMPTZ` as `"2024-01-15T10:30:00Z"`.

### Soft deletes
Domain tables that carry `deleted_at TIMESTAMPTZ`: `accounts`, `transactions`, `categories`, `budgets`, `savings_goals`, `categorization_rules`, `ai_threads`. Queries exclude `deleted_at IS NOT NULL` by default (GORM `gorm.DeletedAt` embedding).

Tables that do **not** carry `deleted_at` (by design):
- `investments` — append-only snapshots; soft-deleting a snapshot silently corrupts historical state.
- `ai_messages` — turn log; cascades on `ai_threads.deleted_at` via thread scoping.
- `ingestion_jobs` — append-only audit trail.
- `pii_store` — hard-delete only (right-to-forget semantics; see PII Isolation above).
- `sessions` — short-lived auth state; hard-delete on signout/expiry.
- `household_members` — uses `left_at` + `purged_at` for the grace-period lifecycle, not `deleted_at`. See [ADR-0007](ADR/0007-member-lifecycle.md).

See also `.claude/rules/database.md`.

## Go Patterns

### Transactions
The GORM connection is opened with `SkipDefaultTransaction: true` (`backend/internal/db/db.go`) — single-row writes skip the implicit per-statement transaction for throughput. **Any service method that writes more than one row, or across more than one table, MUST wrap the work in `db.Transaction(func(tx *gorm.DB) error { ... })`** or torn updates become possible (e.g. account row created but its `pii_store` row fails to insert).

### Dependency injection
```go
// Constructor receives interfaces
func NewAccountService(repo repository.AccountRepository, piiSvc *PIIService) *AccountService

// AI service — note: NO pii access
func NewAIService(repo repository.AIRepository, provider ai.Provider, builder *ContextBuilder) *AIService
```

### Error handling
```go
// Domain errors in service layer
var ErrAccountNotFound = errors.New("account not found")
var ErrDuplicateTransaction = errors.New("duplicate transaction")

// Handler maps to HTTP
switch {
case errors.Is(err, service.ErrAccountNotFound):
    c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "ACCOUNT_NOT_FOUND"})
}
```

### Handler pattern
```go
func (h *AccountHandler) Create(c *gin.Context) {
    var req CreateAccountRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
        return
    }
    account, err := h.service.Create(c.Request.Context(), req)
    if err != nil {
        // map domain error to HTTP
        return
    }
    c.JSON(http.StatusCreated, gin.H{"data": account})
}
```

## Testing Strategy

- **Unit tests**: table-driven, test service logic with real test DB
- **Integration tests**: test HTTP handlers via `httptest`, real PostgreSQL
- **No DB mocks**: use a test PostgreSQL instance (Docker or testcontainers)
- **PII contract tests**: verify `pii_store` is the only location of PII after any operation
- **Test naming**: `Test{Function}_{scenario}` e.g. `TestCreateAccount_DuplicateName`

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `PORT` | No | Backend port (default: 8000) |
| `FRONTEND_URL` | No | CORS origin (default: http://localhost:5173) |
| `MIGRATIONS_PATH` | No | Path to golang-migrate SQL dir (default: `migrations`) |
| `SESSION_SECRET` | Yes (M2.5+) | HMAC key for cookie sessions. Generate with `openssl rand -hex 32`. |
| `PLAID_CLIENT_ID` | No | Plaid API client ID |
| `PLAID_SECRET` | No | Plaid API secret |
| `PLAID_ENV` | No | Plaid environment: sandbox, development, production |
| `CLAUDE_API_KEY` | No | Anthropic API key for AI assistant |
| `OLLAMA_BASE_URL` | No | Ollama server URL (default: http://localhost:11434) |

## Extension Points

### Adding a new ingestion source
1. Implement the `StatementIngester` interface in `service/ingestion/`
2. Register in the ingestion handler's source router
3. Add `source` enum value to transaction model

### Adding a new AI provider
1. Implement the `ai.Provider` interface in `service/ai/`
2. Add provider config to `config.go`
3. Register in `ai_service.go` provider map
4. **Never inject pii_repo into the provider**

### Adding a new household surface
1. Add a method to `service/household/aggregator.go` (e.g. `CashflowByMonth`)
2. Define a return struct with only aggregated fields (no `model.Transaction`, no `model.Account` rows)
3. Add a privacy test in `aggregator_test.go` asserting: private accounts excluded, in-grace members excluded from live aggregates, no raw transactions in return type
4. Add the handler under `/h/...` and have it call only the aggregator — never repositories directly
5. **Never import `pii_repo` into `service/household/`**
