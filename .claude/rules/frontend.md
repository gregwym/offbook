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
- **Design alignment:** frontend work must follow the wireframe in `docs/designs/App Hierarchy v4.html` for functional requirements (which tiles, which table columns, which interactions). Hi-fi treatment (typography, color, spacing polish) lives in `docs/designs/Offbook Hi-Fi v1.html` and is deferred to the paired M9+ frontend milestone unless that milestone is the one being worked on. **Do not invent features** — if a column, tile, action, or page isn't in the wireframe, don't ship it. When the wireframe asks for data the backend can't yet produce, file a backlog issue rather than padding the UI with substitutes.
