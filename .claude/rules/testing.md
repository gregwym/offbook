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
