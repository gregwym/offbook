---
paths:
  - "frontend/**/*.tsx"
  - "frontend/**/*.ts"
---

# Frontend Rules
- State: Zustand stores. No prop drilling beyond 2 levels.
- API calls: always go through src/api/ client layer. Never raw fetch in components.
- Money display: always format via shared AmountDisplay component.
- Types: mirror backend schemas in src/types/. Keep in sync.
- Pages vs Components: pages are route-level only. Reusable UI in components/.
