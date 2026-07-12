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
- **Unit tests:** `vitest` + React Testing Library + MSW (`pnpm test` from `frontend/`; wired into CI's `Frontend lint, test & build` job). Config: `vitest.config.ts` (separate from `vite.config.ts` — tests don't need the Tailwind plugin or dev proxy), setup file `src/test/setup.ts`.
  - `src/test/handlers.ts` + `src/test/fixtures.ts` hold the default "happy path" MSW handlers/data for every endpoint a page or hook hits on mount. Override just the route under test with `server.use(...)` for error/partial-failure cases — don't duplicate the whole handler set.
  - Zustand stores are module-level singletons; `src/test/testUtils.tsx`'s `resetStores()` restores their data fields before each test (`beforeEach`). `renderPage()` wraps a page in `MemoryRouter` (several pages use `<Link>`/`useNavigate`). `setHouseholdScope(id)` flips `scopeStore` into household scope for `/h/*` page tests.
  - Page smoke tests (one per page, colocated as `PageName.test.tsx`) assert `expectHealthySmoke()`: no stuck "Loading…" text and no element carrying the `border-red-200 bg-red-50` error-banner class combo every page uses consistently (see error-banner markup throughout `src/pages/`). This is a real regression class (#266): one failing fan-out call must degrade its own band, not blank the whole page — see `useScopedInsights.test.ts` for the pattern (`Promise.allSettled` per-item, not a top-level `Promise.all`).
