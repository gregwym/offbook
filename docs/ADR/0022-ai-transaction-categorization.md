# ADR 0022: AI Transaction Auto-Categorization

## Status
Accepted (M14 / #366)

## Context

Through M14's earlier issues (#363–#195), categorization is user rules
(`internal/service/categorization`) plus a static Plaid taxonomy lookup
(`plaid_category_map`, ADR unnumbered/#195). Neither covers the long tail:
a merchant string no rule matches and Plaid's `personal_finance_category`
can't map cleanly lands in `categorization_method='plaid_default'` (a weak
guess) or with `category_id IS NULL` (nothing at all). The M9 "Needs review"
banner (#228) already surfaces exactly this set — it just has nothing to
resolve it besides a manual click.

The Product Goal: transactions neither rules nor the Plaid taxonomy could
place get categorized by the user's configured AI provider, without the
user lifting a finger, and without AI ever outranking a human or rule
decision.

Offbook already has one AI capability beyond chat: `DocumentExtractor`
(ADR-0019) — a provider-pluggable capability with its own interface,
implemented per-provider in `internal/service/ai`, resolved per-user via
the settings-backed resolver pattern (ADR-0012). This ADR adds a second
capability, `Categorizer`, following the same shape.

## Decision

### 1. Capability interface lives in `service/categorization`, not `service/ai`

```go
// internal/service/categorization/categorizer.go
type Categorizer interface {
    Categorize(ctx context.Context, candidates []Candidate, options []CategoryOption) ([]Verdict, error)
    Name() string
}
```

Same cycle-avoidance rationale as `ingestion.Extractor` (ADR-0019 §1):
`internal/service/ai` already imports `internal/service` (for
`ContextBuilder`'s dashboard/budget/goal dependencies), and the top-level
`service` package needs to call a `Categorizer` from the batch-pass job —
so the interface can't live in `ai` without an import cycle. It lives next
to the rule engine it complements instead. `ai.ClaudeCategorizer` (new file
`claude_categorizer.go`) implements it, mirroring `claude_extractor.go`'s
single-shot Messages-API-call style (not streaming).

### 2. Resolution order: manual > rule > AI > plaid_default

Enforced structurally, not by a priority field:

- **manual** and **rule** are decided synchronously at write time (create,
  edit, Plaid sync drain) and are never revisited by this pass — the batch
  scan's WHERE clause only selects rows where `category_id IS NULL OR
  categorization_method = 'plaid_default'`. A manually-picked or
  rule-matched row is structurally invisible to the categorizer.
- **AI** runs after — a new `ai_eligible` scope on
  `TransactionRepository.ListForCategorizationScope` (already the bulk-scan
  primitive `CategorizationRuleService.Apply` uses) selects exactly that
  set, restricted to `kind = 'flow'` (trade legs / opening balances /
  adjustments have no merchant text and are out of scope for a spending
  categorizer).
- **plaid_default** is what a row keeps if AI declines (no confident
  verdict, no provider configured, or the daily budget is exhausted) — the
  row is unchanged, so it keeps surfacing in "Needs review" exactly as
  before this feature shipped.
- If the user later adds a **rule** that matches an AI-categorized row, the
  existing bulk-`Apply` re-categorize pass overwrites it — `Apply` already
  treats `categorization_method='ai'` the same as `'plaid_default'` (only
  `'manual'` is skipped), so rule-over-AI precedence needs no new code.

### 3. Post-import batch pass, not an inline sync-path call

A new in-app job (`internal/service/jobs`, ADR-0020), registered in
`cmd/server/main.go` alongside `plaid-transaction-sync`, runs once daily
(staggered `InitialDelay` so it operates on same-day-synced rows) over
every user who has opted in (`user_settings.auto_categorize`). It does
**not** touch the Plaid sync drain (`plaid/transaction_mapping.go`) or run
inline with any request — keeping the sync path's existing transaction
boundaries untouched and keeping AI latency off every import request.

`RunCategorizationPass` (`internal/service/transaction_categorization.go`)
is the reusable per-user pass: it never imports `ai` directly (cycle, see
§1) — the job in `main.go` resolves a `categorization.Categorizer` per user
(mirroring `router.go`'s `extractorResolver`: Claude-only today, `nil` for
Ollama/OpenAI users with a comment marking the fast-follow) and passes it
in, exactly like the Plaid job builds its own service instances rather than
sharing the HTTP-facing ones.

### 4. Payload minimization

`categorization.Candidate` carries exactly three fields per row:
`MerchantName`, `DescriptionClean`, `AmountSign` ("debit" | "credit" — the
raw signed amount never leaves the process). No account ID, no date, no raw
`Description`, no user identifier. `ClaudeCategorizer.Categorize` batches N
candidates (`AI_CATEGORIZE_BATCH_SIZE`, default 20) plus the caller's
`CategoryOption` list (id + slug) into one Messages call — "batch prompt
sends N transactions per call, not one-per-call" is a cost requirement, not
just a latency one. The PII ban is enforced the same way ADR-0003 enforces
it everywhere else in this package: `noimport_test.go`'s import-graph walk
already covers every file under `internal/service/ai`, including
`claude_categorizer.go` — no `pii_repo` edge is reachable from this new
file without failing that test.

### 5. Confidence gate reuses the existing "Needs review" flow verbatim

There is no new confidence column on `transactions` and no new review UI.
A verdict below `AI_CATEGORIZE_CONFIDENCE_THRESHOLD` (default 0.6) is
**not written** to the row — it stays `category_id IS NULL` or
`categorization_method='plaid_default'`, i.e. exactly the state "Needs
review" (#228) already filters on. A verdict at or above threshold writes
`category_id` + `categorization_method='ai'` and is fully committed — it
does not need review, matching "no silent low-confidence commits" (a
confident AI verdict is not silent; it is exactly as committed as a rule
match).

### 6. Merchant → category verdict cache, one row per (user, merchant)

`ai_categorization_verdicts` (migration 000029): `UNIQUE (user_id,
merchant_key)`, upserted on every AI call. `merchant_key` is
`categorization.NormalizeMerchantKey(merchantName, descriptionClean)` —
uppercased, trimmed, `MerchantName` preferred over `DescriptionClean` (same
priority the rule engine and `openRuleFromTxn`'s rule-seeding already use).
Before calling the AI, the pass checks the cache first: a hit above
threshold writes the row for free (no API call, doesn't count against the
daily budget); a hit below threshold is skipped without a repeat API call
either (the merchant already has a known-poor verdict — retrying it daily
would just burn budget for the same answer). Only a cache **miss** goes
into an AI batch.

**Promote to rule** is frontend-only, no new backend endpoint: a new `GET
/api/v1/categorization-verdicts` lists the current user's cache, and the
Rules page seeds `RuleFormModal` from a cache entry's `merchant_key` +
`category_id` exactly the way `TransactionsPage.openRuleFromTxn` already
seeds it from a live transaction (`match_type: 'contains'`, priority = max
existing + 10). Once the user submits, it's a normal rule — nothing in the
cache tracks "promoted" state; a promoted merchant simply stops reaching
this pass at all, because the new rule intercepts it at scope-selection
time (§2).

### 7. Per-instance daily AI-call budget

`AI_CATEGORIZE_DAILY_BUDGET` (default 200 calls/day, config only — no DB
row, no owner-facing knob yet) caps how many `Categorize` batch calls the
job makes in one run, shared across every opted-in user in that run (a
single in-memory counter, safe because the job runs single-goroutine —
see `jobs.Runner.loop`, one goroutine per job). At `AI_CATEGORIZE_BATCH_SIZE`
= 20 candidates/call, the default budget caps a day's AI categorization at
4,000 transactions instance-wide — well beyond one household's daily
transaction volume, bounding worst-case cost for a multi-tenant instance
without needing a persistent counter table (the job's daily cadence *is*
the reset). Rows that don't fit in the budget are simply left as they were
— unprocessed, to be picked up by tomorrow's run — "stay uncategorized
until tomorrow" per the acceptance criteria, no partial-row state to track.

### 8. Opt-in, mirroring `auto_price_refresh`

`user_settings.auto_categorize BOOLEAN NOT NULL DEFAULT FALSE` (migration
000029), same shape and same rationale as `auto_price_refresh` (ADR-0014
§3): background egress of the user's transaction merchant strings without
a per-request click is a stored-consent decision, not a default-on one.
Settings UI toggle mirrors `PriceSettingsSection` exactly.

## Rationale

- **Interface in `categorization`, impl in `ai`** — the only shape that
  doesn't create an import cycle given `ai` already depends on `service`.
- **Scope reuse (`ListForCategorizationScope` + a new case) over a new
  query path** — the exact same rows "Needs review" already tracks are the
  rows this pass should improve; reusing the query means the two features
  can never drift on what counts as "needs a category."
- **Batch job over an inline sync-path hook** — decouples AI latency/cost
  from the request that triggers a sync, and never touches the Plaid
  drain's existing DB-transaction structure (`SyncTransactions`'s Phase 2
  is already delicate; ADR-0011 covers its partial-success handling).
- **Verdict cache keyed by merchant, not by transaction** — the same
  merchant recurs across many transactions (subscriptions, regular
  grocery runs); paying for one AI call per merchant instead of per row is
  the entire cost story that makes a daily instance-wide budget viable.
- **No new confidence column** — a database column that exists only to
  gate one write path more of complexity than the existing plaid_default /
  uncategorized state already provides for the exact same purpose.

## Consequences

- A user with no AI provider configured (or who hasn't opted in via
  `auto_categorize`) sees no behavior change at all — rows keep landing in
  `plaid_default`/uncategorized exactly as before this ADR.
- Ollama and OpenAI-compatible categorization are explicitly deferred, same
  posture as `extractorResolver` for document extraction (ADR-0019 §6) —
  `nil` categorizer for those protocols, silently skipped by the batch
  pass (not an error).
- The daily budget is a static config value, not a per-instance UI-editable
  setting — acceptable for a first cut; if a self-hoster needs a different
  cap, `AI_CATEGORIZE_DAILY_BUDGET` is an env var like every other instance
  tuning knob in this codebase (`LOW_DISK_THRESHOLD_PCT`, etc.).
- `ai_categorization_verdicts` never soft-deletes and has no purge job — it
  is a bounded-size cache (one row per distinct merchant per user), not an
  audit trail; unlike `ingestion_jobs` it carries no large payload to
  reclaim.

## Out of Scope
- Re-categorizing rows already at `categorization_method` in {`manual`,
  `rule`}.
- AI-generated *rules* — only the user, via "promote to rule," turns a
  cache entry into a rule.
- Any change to the advisor chat surface (`ai.Service.SendMessage`) or the
  document extractor.
