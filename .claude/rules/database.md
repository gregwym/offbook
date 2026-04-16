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
