---
paths:
  - "backend/**/*_test.go"
  - "frontend/**/*.test.ts"
  - "frontend/**/*.test.tsx"
---

# Testing Rules
- Go tests: table-driven. Use subtests with t.Run().
- Integration tests: use a test PostgreSQL database (Docker or testcontainers).
- Test names: Test{Function}_{scenario} e.g. TestCreateAccount_DuplicateName.
- No mocks for the database — use a real test DB.
- PII tests: verify pii_store is the only table containing PII after any operation.
- Aggregator privacy tests: when modifying `internal/service/household/`, the integration suite must assert (a) private accounts excluded, (b) `balance_only` excluded from category-level aggregates, (c) in-grace members excluded from live aggregates but present in historical, (d) return types contain no raw transaction rows (reflection check), (e) `AIContext` for member A never includes member B's non-shared threads.
- Multi-tenant tests: any new repository read path must include a test that a different user's data is not returned.
