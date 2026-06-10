# ADR-0014: Pluggable Price & FX Providers

**Status:** Accepted

**Context date:** 2026-06-10

## Context

ADR-0013/ADR-0017 made every valuation a pure function of stored facts:
`value(t) = quantity_asof(t) × price_asof(t)`. Quantities flow from the
transaction ledger; prices flow from the `prices` time series. But the only
writers of `prices` are Plaid sync and statement import — so any asset not
covered by those (manually tracked equities and crypto, foreign-currency
cash needing FX) freezes at its last imported price. The valuation layer
correctly reports those positions stale/unpriced (#282, #339), which makes
the gap *visible* — this ADR is about making it *rare*.

The deferral note in M10 reserved this ADR for "Pluggable Tier-3 price
provider (Yahoo, Polygon, ECB, CoinGecko)."

## Decision

### 1. Providers are a new seam: `internal/service/prices`

Mirror the `service/ai` provider pattern: a small interface, concrete
implementations per upstream source, a service that orchestrates them.

```go
type Provider interface {
    // Name doubles as the prices.source value ('coingecko', 'ecb', …).
    Name() string
    // Supports reports whether this provider can quote the asset at all.
    Supports(a model.Asset) bool
    // Fetch returns current observations for the supported assets, priced
    // in the quote asset. Unquotable assets are silently omitted from the
    // result — the caller treats them as skipped.
    Fetch(ctx context.Context, assets []model.Asset, quote model.Asset) ([]Quote, error)
}
```

Providers **only write `prices` rows** (via the orchestrating service).
They never touch `positions` or `transactions` — quantity remains
ledger-derived per ADR-0017. A provider failure or gap degrades to the
existing stale/unpriced signal; it never coerces to $0 and never blocks
the request beyond reporting the error.

### 2. Refresh is user-initiated first, scheduled later

Phases (tracked in #338):

1. **Phase 1 — seam + CoinGecko + manual refresh.** `POST /prices/refresh`
   refreshes prices for the assets the *requesting user currently holds*
   (positions ≠ 0), in the user's primary currency. A "Refresh prices"
   button on Insights drives it. Crypto only (CoinGecko's free, keyless
   `/simple/price` endpoint).
2. **Phase 2 — FX.** Daily fiat reference rates (ECB or equivalent) so
   non-primary-currency cash prices into the user's primary currency.
3. **Phase 3 — scheduled refresh.** Periodic refresh for all held assets
   across users, respecting provider rate limits.

### 3. Egress and privacy

What leaves the box is **the symbol list only** — never quantities, never
amounts, never account metadata, never PII. In Phase 1 the egress is
strictly user-initiated: clicking "Refresh prices" *is* the consent, the
same model as choosing a cloud AI provider (ADR-0019's "locality is the
user's provider choice"). Phase 3's background refresh will require a
persistent opt-in setting before it ships.

A symbol list is still a fingerprint of the user's portfolio composition.
Providers requiring an API key (Polygon, etc.) tie that fingerprint to an
account and will be opt-in per provider with the key stored encrypted,
like the Claude API key. Keyless providers (CoinGecko, ECB) carry only
IP-level exposure.

### 4. Idempotency and provenance

`prices` is append-only with upsert-in-place on
`(asset_id, quote_asset_id, as_of, source)` — re-refreshing within the
same observation instant updates rather than duplicates, and provider
observations never collide with `plaid`/`manual`/import-sourced rows for
the same instant. `source` = `Provider.Name()` keeps every observation
attributable.

### 5. Symbol mapping is static and reviewable

CoinGecko keys quotes by its own coin IDs (`bitcoin`, not `BTC`). The
mapping ships as a small, code-reviewed table of major assets rather than
a runtime call to the (huge, ambiguous) `/coins/list` endpoint. A held
symbol outside the table is reported as *skipped* in the refresh result —
visible, not silent — and extending the table is a one-line PR.

## Alternatives considered

- **Price lookup at valuation time** (no stored series): breaks
  `price_asof(t)` history, couples every page load to third parties,
  and silently re-fingerprints the portfolio on every render. Rejected.
- **One mega-provider with internal routing:** the registry already *is*
  the router; per-source implementations keep failures, rate limits, and
  consent per provider.
- **Runtime symbol resolution via provider search APIs:** ambiguous
  (ticker collisions across exchanges/chains) and unreviewable. The
  static map trades coverage for correctness; skipped symbols are
  surfaced to the user.

## Consequences

- Net worth, allocation, and account balances stay current between
  imports for covered assets; gaps surface through the existing
  stale/unpriced signals (#339) instead of silent staleness.
- Each new source is a small `Provider` implementation plus registry
  wiring — no changes to valuation or storage.
- The static symbol map is a maintenance surface; acceptable because
  misses are visible in the refresh result.
