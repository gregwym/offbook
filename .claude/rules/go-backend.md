---
paths:
  - "backend/**/*.go"
---

# Go Backend Rules
- Handlers: parse request → call service → return JSON. No business logic in handlers.
- Services: receive repository interfaces as constructor args. Never instantiate DB directly.
- Repositories: GORM calls only. Return domain models, not GORM-specific types.
- Money: shopspring/decimal everywhere. Never float64 for monetary values.
- Errors: services return domain errors (ErrAccountNotFound, ErrDuplicateTransaction).
  Handlers map domain errors to HTTP status codes.
- Naming: receiver names are short (1-2 chars). Interface names don't start with I.
