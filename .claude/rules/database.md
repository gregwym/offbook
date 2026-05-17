---
paths:
  - "backend/internal/model/**"
  - "backend/migrations/**"
---

# Database Rules
- All monetary columns: NUMERIC(30, 18). No FLOAT, no DOUBLE, no INTEGER cents.
- Schema changes: ALWAYS via golang-migrate. Never manual SQL against the DB.
- PII isolation: pii_store is the ONLY table for PII. Main tables use labels, not real names.
- Soft deletes: deleted_at TIMESTAMPTZ. All queries exclude deleted rows by default.
- Timestamps: always TIMESTAMPTZ, never TIMESTAMP.
- Indexes: add for any column used in WHERE, JOIN, or ORDER BY.
- Tenancy: every user-owned domain table (accounts, transactions, budgets, savings_goals, investments, ai_threads, etc.) has `user_id BIGINT NOT NULL REFERENCES users(id)`. Cross-user reads go through the household aggregator (see ADR-0008), never directly across `user_id`.
- Soft-delete-safe uniqueness: partial unique indexes WHERE deleted_at IS NULL. Same pattern for purge-soft tables like household_members (WHERE purged_at IS NULL).
