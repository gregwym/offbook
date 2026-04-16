# Offbook Architecture

Privacy-first personal finance app. All data stays local. Self-hostable via Docker.

## Guiding Principles

1. **Privacy by architecture** — PII lives in a dedicated `pii_store` table, isolated from all other queries. The AI service layer has no access to PII.
2. **Precision over convenience** — All monetary values use PostgreSQL `NUMERIC(30,18)` and Go `shopspring/decimal`. No floats, no integer cents. Required for crypto (18 decimal places).
3. **Local-first** — Runs on localhost. No external services except Plaid (opt-in) and AI providers (opt-in, data-minimized).
4. **Soft deletes everywhere** — Financial data is never hard-deleted. `deleted_at TIMESTAMPTZ` on all domain tables.

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
│   ├── db/db.go                  # GORM connection, migration runner
│   ├── model/                    # GORM models — one file per domain entity
│   ├── handler/                  # Gin handlers — thin, parse request → call service → respond
│   ├── service/                  # Business logic — receives repo interfaces
│   │   ├── ai/                   # AI provider protocol, context builder, service
│   │   └── pii_service.go        # ONLY service with pii_repo access
│   ├── repository/               # DB access — interfaces + GORM implementations
│   │   └── pii_repo.go           # Only injected into pii_service
│   └── router/router.go          # Route registration, middleware
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

## PII Isolation

### Table: `pii_store`
```sql
id            SERIAL PRIMARY KEY
entity_type   TEXT NOT NULL      -- 'account' | 'transaction' | 'institution'
entity_id     INTEGER NOT NULL
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
- Frontend accesses PII via explicit `/accounts/:id/pii` endpoint — deliberate, auditable

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

### Core tables
- **accounts** — `id, name, institution_slug, account_type, currency, balance, last_four, plaid_account_id, plaid_item_id, is_active, created_at, updated_at, deleted_at`
- **transactions** — `id, account_id, category_id, amount, currency, description, description_clean, merchant_name, transaction_date, posted_date, source, external_id, plaid_transaction_id, categorization_method, is_transfer, transfer_pair_id, notes, created_at, updated_at, deleted_at`
  - `source`: `'plaid' | 'csv' | 'pdf' | 'manual'`
  - `UNIQUE(account_id, external_id)` for deduplication
- **categories** — hierarchical via `parent_id`, seeded with ~20 system categories
- **categorization_rules** — `pattern, category_id, match_type ('contains'|'regex'|'exact'), priority`
- **budgets** — `category_id, period ('monthly'|'weekly'|'annual'), amount, rollover, is_active`
- **savings_goals** — `name, target_amount, current_amount, target_date, account_id`
- **investments** — append-only snapshots: `account_id, ticker, name, asset_class, quantity, cost_basis, market_value, snapshot_date, source`
- **ai_conversations** — `id, title, created_at, updated_at`
- **ai_messages** — `conversation_id, role, content, context_snapshot (JSONB), provider, model_name, created_at`
- **pii_store** — see PII Isolation section

## API Conventions

### Base path
All routes under `/api/v1/`.

### Response format
```json
// Success (list)
{"data": [...], "total": 42}

// Success (single)
{"data": {...}}

// Error
{"error": "Human-readable message", "code": "MACHINE_READABLE_CODE"}
```

### Pagination
Query params: `?limit=50&offset=0`. Default limit: 50, max: 200.

### Dates
RFC3339 in JSON. `DATE` columns as `"2024-01-15"`. `TIMESTAMPTZ` as `"2024-01-15T10:30:00Z"`.

### Soft deletes
All queries exclude `deleted_at IS NOT NULL` by default. GORM handles this via model embedding.

## Go Patterns

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
