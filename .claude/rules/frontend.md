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
- Scopes: the sidebar renders ONLY the active scope's routes. Two scopes — personal (`👤`) and household (`🏠`). State lives in a zustand `scopeStore`, server-synced via `GET/PATCH /me/scope`. Household routes live under `/h/*` and only render when the user is a household member.
