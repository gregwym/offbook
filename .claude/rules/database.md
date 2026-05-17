---
paths:
  - "backend/internal/model/**"
  - "backend/migrations/**"
---

# Database Rules
- All monetary columns: NUMERIC(30, 18). No FLOAT, no DOUBLE, no INTEGER cents.
- Schema changes: ALWAYS via golang-migrate. Never manual SQL against the DB.
- PII isolation: pii_store is the ONLY table for PII. Main tables use labels, not real names.
- Soft deletes apply to financial **domain** tables: `accounts`, `transactions`, `categories`, `budgets`, `savings_goals`, `categorization_rules`, `ai_threads`. Each has `deleted_at TIMESTAMPTZ`; queries exclude deleted rows by default (GORM `gorm.DeletedAt`). Append-only / audit / write-once tables (`investments`, `ai_messages`, `ingestion_jobs`, `pii_store`, `sessions`) hard-delete or never delete — see `docs/ARCHITECTURE.md` for the per-table rationale. Don't add `deleted_at` to a snapshot/audit table; soft-deleting a snapshot silently corrupts historical state.
- Transactions: the GORM connection sets `SkipDefaultTransaction: true` (perf — skips the implicit per-write transaction). Single-row writes are fine; any service method that writes more than one row, or across more than one table, MUST wrap the work in `db.Transaction(func(tx *gorm.DB) error { ... })` so a mid-flight failure rolls back.
- Timestamps: always TIMESTAMPTZ, never TIMESTAMP.
- Indexes: add for any column used in WHERE, JOIN, or ORDER BY.
- Tenancy: every user-owned domain table (accounts, transactions, budgets, savings_goals, investments, ai_threads, etc.) has `user_id BIGINT NOT NULL REFERENCES users(id)`. Cross-user reads go through the household aggregator (see ADR-0008), never directly across `user_id`.
- Soft-delete-safe uniqueness: partial unique indexes WHERE deleted_at IS NULL. Same pattern for purge-soft tables like household_members (WHERE purged_at IS NULL).
