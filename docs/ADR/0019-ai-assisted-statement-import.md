# ADR 0019: AI-Assisted, Format-Adaptive Statement Import

## Status
Proposed.

Extends [ADR-0004](0004-plaid-before-csv.md) (CSV after Plaid), [ADR-0005](0005-pluggable-ai.md) / [ADR-0012](0012-ai-provider-resolution-and-household-routing.md) (pluggable AI providers + per-user resolution), and the M10 invariant that **the app never invents transactions** ([ADR-0013](0013-position-based-account-model.md)). Builds on the CSV import vertical slice shipped in #330/#331.

## Context

CSV import (#331) ships with deterministic parsing: header auto-detection with a manual column-mapping fallback when autodetect fails. It works, but it has three gaps against the owner's product vision:

1. **Format rigidity.** The user can be asked to map columns. PDF and photo statements have no usable column structure at all. Every new bank layout is a potential dead end.
2. **One format per code path.** CSV, PDF, and photo would each need bespoke parsing logic.
3. **No accuracy signal.** A mis-parsed row looks identical to a correct one until the user eyeballs it.

The owner reframed import (June 2026):

- **All import kinds auto-adapt format** — including CSV. The user never specifies a layout.
- **The user's chosen AI provider** is used to ensure accurate extraction across arbitrary formats.
- **Low-confidence extractions are surfaced**, never silently committed.
- **Providers stay behind a common Go interface** so new ones plug in as adapters.

This collides with two load-bearing invariants:

- **PII isolation.** A raw statement (CSV/PDF/photo) contains the exact PII that `pii_store` isolates — account numbers, holder names, addresses. The entire `service/ai` package is architected (and test-enforced via `noimport_test.go`) so providers *never* see PII. Routing a statement through a cloud provider is a deliberate, new egress event.
- **"The app never invents transactions."** LLMs hallucinate; money is `NUMERIC(30,18)`. An LLM's extracted amounts/dates cannot be trusted as facts.

## Decision

Introduce an **Extractor pipeline** in front of the existing import trust boundary. The `ingestion.ParsedRow` → import-service `preview → dedup → commit` flow is unchanged and remains the only path that writes transactions. We add a strategy layer that *produces* `ParsedRow`s from any input.

```
file (csv / pdf / image)
   │
   ▼
Extractor registry  ── tries in order, escalates on low confidence:
   1. deterministic  (CSV headers, PDF text)      free · exact · on-box
   2. AI extractor   (user's configured provider) "auto-adapt any format"
   │  emits ParsedRow[] + per-row confidence + document metadata (detected totals)
   ▼
Deterministic re-validation + reconciliation
   · every AI-extracted field re-parsed through the existing date/decimal validators
   · row sum reconciled against any statement total/balance-delta the extractor detected
   ▼
ImportStatement pipeline (today's ImportCSV, generalized):
   preview → confidence-gated review → commit
```

### 1. Deterministic-first, AI-fallback

The deterministic parser runs first on every import. AI is invoked only when deterministic parsing **fails or is low-confidence** (CSV autodetect misses a required field; PDF yields no usable text; a photo has no text layer at all). Rationale: CSV and text-PDF are already exact, free, and on-box — spending tokens, egress, and hallucination risk on them is strictly worse. The user still never specifies a format: autodetect handles the common case, AI handles the long tail.

### 2. Extraction is a distinct trust context from chat

We do **not** overload `ai.Provider.Stream` (the chat surface, which is PII-banned by construction). We add a sibling capability in `service/ai`:

```go
// DocumentExtractor turns raw statement bytes into candidate rows.
// Implementations: claude (vision + text), ollama (vision-capable models).
type DocumentExtractor interface {
    Extract(ctx context.Context, doc Document) (*Extraction, error)
    Name() string
}

type Document struct {
    Filename string
    MIME     string // text/csv, application/pdf, image/png, image/jpeg
    Bytes    []byte
}

type Extraction struct {
    Rows      []ExtractedRow   // each carries its own Confidence
    DocTotals []decimal.Decimal // detected statement totals, for reconciliation
    Notes     string
}

type ExtractedRow struct {
    Date        string          // raw; re-parsed deterministically downstream
    Amount      string          // raw; re-parsed to decimal downstream
    Description string
    Confidence  float64         // 0–1, provider-reported, treated as a hint only
}
```

Extraction gets its **own egress + consent rules**, separate from the chat provider's hard PII ban. The `ProviderResolver` (ADR-0012) is reused to pick the user's provider; a parallel resolver method (or a capability check) selects the extraction-capable provider.

### 3. AI output is data, never truth

Every field an extractor returns is re-parsed through the **existing deterministic validators** (`ingestion.parseDate`, `parseAmount`). A row whose amount/date fails re-parsing is marked `error`, exactly as a malformed CSV row is today. Where the extractor reports a document total, the sum of extracted rows is **reconciled** against it; a mismatch flags the whole batch for review. This is the mechanical guard for "never invent transactions" — the LLM can only *propose* rows; deterministic code decides whether each is admissible.

### 4. Confidence surfaces in the existing preview

`ImportRowResult` gains `confidence float64` and `needs_review bool`. The preview modal sorts low-confidence / unreconciled rows to the top and blocks one-click commit when any row needs review (the user must acknowledge). No new surface — the preview step already exists and already writes nothing.

### 5. Egress posture: cloud now, redaction later, locality is the user's provider choice

- **Now:** the user's configured provider parses the statement. Ollama users stay fully on-box; Claude users perform a deliberate, **consented** upload (explicit "this sends your statement to <provider>" confirmation per import) recorded in an audit trail.
- **Locality is the user's choice**, expressed by which provider they configure — we do not impose a local-only restriction.
- **Future:** a redact-then-send pass (strip account numbers/names before a cloud call) is a planned enhancement, not a launch blocker. It is explicitly deferred because it is brittle for free-text descriptions and effectively impossible for photos.

### 6. Provider plugins stay Go-native

New extraction providers are **Go adapters** implementing `DocumentExtractor`, selected via the existing resolver and per-user settings. We reject a Node.js adapter sidecar: it would add a third runtime, a deploy surface, and a new trust boundary to a Go + Postgres + React stack for no capability the Go interface can't express.

## Consequences

- The `ingestion` package stays the neutral row shape; a new `extractor` concern sits beside it. The import service method `ImportCSV` is generalized to `ImportStatement(..., source)` so `source` ∈ {`csv`, `pdf`} (and the AI path tags provenance) is set correctly. `external_id` prefixing stays per-source for idempotent re-import.
- Two provider implementations must now support a second capability (extraction/vision). Providers that can't (no vision model configured) are simply unavailable for image import, surfaced in settings — mirroring how chat providers are hidden when unconfigured.
- A statement upload to a cloud provider is a genuine privacy event. It is gated behind explicit per-import consent and an audit record; the default-deny posture of the rest of the app is preserved by making the egress opt-in at the moment of import.
- Reconciliation needs a detected document total to be meaningful; when none is found, the batch falls back to per-row confidence + mandatory review rather than a hard total check.
- This supersedes the "defer photo, ship PDF-text only" interim decision: photo is no longer a separate OCR-dependency question — it routes through the AI extractor's vision capability like any other unparseable input.

## Rollout

Phased; each phase is independently shippable and merges via its own PR.

1. **Extractor seam (no AI).** Generalize `ImportCSV` → `ImportStatement`; introduce the `Extractor` registry with the deterministic extractor only; add `confidence`/`needs_review` to `ImportRowResult` (deterministic rows = confidence 1, never needs review). Pure refactor; behavior identical. 
2. **Text-PDF extractor.** Pure-Go PDF text extraction → `ParsedRow`s through the same validators. No AI, no egress.
3. **AI extraction capability.** Add `DocumentExtractor` to the provider interface; implement for the configured provider; wire the fallback escalation + consent + audit + reconciliation; extend the import modal for confidence review and image upload.
4. **(Future)** Redact-then-send pre-pass for cloud egress.
