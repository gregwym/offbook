# ADR 0012: AI Provider Resolution + Household Context Routing

## Status
Accepted

## Context
ADR-0005 ratified the `ai.Provider` interface and noted that "users switch between them at runtime via the settings page." When M7 + the M2.5-deferred Household AI Advisor (#167) actually shipped, four concrete decisions had to be made that ADR-0005 doesn't cover:

1. **Where do per-user API keys live?** Instance-wide environment variables don't fit a multi-tenant deployment where each user pays for their own Claude tier.
2. **How does `ai.Service.SendMessage` know which provider to use?** It runs once per request and can't read process env at every call without ignoring user settings.
3. **How does the same service handle personal vs household threads?** They differ in (a) authorization audience, (b) which context builder feeds the system prompt, (c) who can read prior turns.
4. **How does a shared thread attribute "who said what" without leaking PII?** Multiple users post to the same thread; assistant messages have no author.

This ADR documents those decisions as they're now embodied in `service/ai/`, `service/user_settings_service.go`, `repository/user_settings_repo.go`, and `router/ai_household_access.go`.

## Decision

### 1. Per-user API keys (#131)

A `user_settings` table holds one row per user with three knobs:
- `claude_api_key_enc BYTEA` — AES-256-GCM ciphertext (the existing `crypto.SecretBox` pattern from ADR-0010).
- `ollama_base_url TEXT NULL`
- `preferred_provider TEXT CHECK ('claude' | 'ollama')`

**Encryption key derivation.** The SecretBox key is `SHA-256(SESSION_SECRET)` — 32 bytes. This avoids a new operator-facing secret. `SESSION_SECRET` is already required from M2.5+; reusing it via a one-way hash keeps the deployment story unchanged while giving the SecretBox a domain-distinct key (any future cookie/session attack on `SESSION_SECRET` doesn't directly expose a usable AES key).

**Redact-on-read.** `GET /api/v1/me/settings` returns `claude_api_key_set: bool` — never the plaintext. The plaintext is only readable inside the process via `UserSettingsService.Resolve(ctx, userID)`, which the router-side provider resolver calls. The HTTP handler doesn't have access to plaintext at any point.

### 2. Provider resolution per call

`ai.Service` does NOT hold a single `Provider`. It holds a `ProviderResolver` interface:

```go
type ProviderResolver interface {
    For(ctx context.Context, userID int64) (Provider, error)
}
```

Production wiring (`router.aiProviderResolver`):
1. Looks up the user's `user_settings` row.
2. If `preferred_provider == "ollama"`, constructs an Ollama provider (using the user's URL or the env default).
3. Else if the user has a Claude key set, decrypts it and constructs a Claude provider with that key.
4. Else falls back to an env-configured Claude provider (`CLAUDE_API_KEY`) — keeps the single-user local-deploy story working without anyone visiting Settings.
5. Returns nil if nothing is configured; `SendMessage` maps that to `ErrNoProvider` (503 `NO_AI_PROVIDER`).

Tests use a `StaticResolver(p)` helper that always returns `p` so unit suites don't need to spin up the settings stack.

### 3. Household routing via a thin `HouseholdAccess` interface

The AI package must not depend on `service/household` directly (architectural rule from `.claude/rules/go-backend.md`: peer service packages don't import each other; the router wires them). So `ai.Service` declares a slim interface:

```go
type HouseholdAccess interface {
    ActiveMembership(ctx, userID, householdID) (*model.HouseholdMember, bool, error)
    BuildAIContext(ctx, userID, householdID) (json.RawMessage, error)
}
```

A `router/ai_household_access.go` adapter implements it on top of `repository.HouseholdMemberRepository` + `household.Aggregator`. The aggregator already returns `HouseholdAIContext` (anonymized, no PII, no raw txns by ADR-0008's reflection test) — the adapter just marshals it.

**Authorization tree** (see `service/ai/household.go`):
- Personal endpoints (`/ai/...`) gate on `thread.user_id = caller`. Unchanged from M7.
- Household endpoints (`/h/ai/...`) gate on (a) `ActiveMembership(caller, household) == true` AND (b) the thread is either owned by caller OR `shared_with_household=true AND household_id=caller's household`.
- In-grace members are excluded from both gates — they get `ErrNotHouseholdMember` (handler maps to 403 `NOT_HOUSEHOLD_MEMBER`), matching the aggregator's lifecycle filter (ADR-0007).

**Context source selection.** `SendMessage` (personal) uses the personal `ContextBuilder.Build(userID)`. `SendSharedMessage` (household) uses `HouseholdAccess.BuildAIContext(userID, householdID)`. The two paths embed different JSON shapes in the system prompt; that's deliberate — household threads see household-scope aggregates only, personal threads see personal-scope only.

### 4. Per-message authorship on shared threads (#167, migration 000011)

`ai_messages` gained a nullable `user_id` column. Semantics:
- `role='user'` → `user_id` = the poster's id. Required for shared threads where multiple members write.
- `role='assistant'` → `user_id` = NULL. Assistant messages have no human author.
- Pre-migration rows → `user_id` = NULL (un-attributable; backfill not attempted because shared threads didn't exist before this migration).

The frontend uses `user_id` to render `You · #<id>` on the poster's own bubbles in shared threads. Cross-member display names aren't in scope — the user_id is enough to disambiguate "is this me" vs "is this a peer."

## Rationale

- **One row per user vs columns on `users`** — keeps the auth/identity table free of feature-specific bytes. `user_settings` can grow without churning the hot `users` table.
- **Reuse `SESSION_SECRET` for SecretBox key** — minimizes operator surface area (one secret, not two). The cost of a `SESSION_SECRET` compromise already grants session forgery; granting access to stored Claude keys is a strictly smaller incremental loss.
- **ProviderResolver over a global Provider** — required for per-user keys, but also useful for future per-thread overrides ("use Ollama for this thread") without changing the service contract.
- **Adapter pattern for `HouseholdAccess`** — preserves the "services don't import each other" invariant from `.claude/rules/go-backend.md`. The adapter is router-level wiring, not service code.
- **`user_id` on `ai_messages` rather than a join table** — simpler. Messages are append-only and small; a single FK column with `ON DELETE SET NULL` covers user-account deletion without orphaning the message history.

## Consequences

- The router has two non-trivial new files: `aiProviderResolver` (~30 LOC) and `ai_household_access.go` (~60 LOC). Both are pure wiring with no business logic, so the bus factor stays low.
- Migration 000011 is irreversible in the sense that backfilling `user_id` post-rollback is impossible without external context; the down migration is safe (it just drops the column).
- `UserSettingsService.Resolve` is the only path to a decrypted Claude key inside the process. It must NEVER be called from a handler — only from router-level wiring. Enforced by code review; no compile-time check.
- The personal `ContextBuilder` and the aggregator's `AIContext` produce different shapes (personal: spend-by-category + holdings + budgets + goals; household: net worth + income + spending + shared threads list). Frontend "Context sent" panels reflect this by labeling differently (`AIChatSurface.notSent` is a prop).
- Adding a third provider (OpenAI, llama.cpp) still only requires implementing `ai.Provider` — but it now ALSO needs a row-shape extension on `user_settings` if it has API-key-style credentials. ADR-0005's promise still holds for the provider interface itself.

## Out of Scope
- A "promote personal thread to shared" toggle — would require a multi-user-context migration on existing thread state and isn't in any current product spec.
- Per-thread provider overrides (use Claude for thread A, Ollama for thread B). The resolver shape supports it; the UI doesn't.
- Caching the decrypted Claude key in-process. Decryption is a single AES-GCM op per `SendMessage`; not worth the security tradeoff.
