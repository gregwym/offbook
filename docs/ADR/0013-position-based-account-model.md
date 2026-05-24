# ADR 0013: Position-Based Account Model

## Status
Proposed

## Context

The schema today (per [ADR-0002](0002-postgres-over-sqlite.md) and the M1/M6 designs in `docs/ARCHITECTURE.md`) models account value as a **two-shape compromise**:

- **Cash-like accounts** (`checking | savings | credit_card | loan | cash`) carry a scalar `accounts.balance NUMERIC(30,18)` denominated in `accounts.currency`. There is no representation of holdings — value *is* the scalar.
- **Investment-like accounts** (`investment | crypto`) carry holdings in a separate `investments` table — append-only snapshots with `ticker`, `quantity`, `cost_basis`, `market_value`, `snapshot_date`. The parent `accounts.balance` is *also* populated (by Plaid sync or manual entry), redundantly.

This works for the current product surface but fails to model how value actually composes:

1. **Multi-currency in one account is not representable.** A Schwab account holding $5,000 USD + €200 EUR + 10 AAPL shares can't be one `accounts` row — `currency` is singular and `balance` is scalar.
2. **Cash is not a first-class position.** Brokerage cash sleeves have no clean home. There's no FX layer — each account is valued in its own currency with no consistent reduction to a primary currency.
3. **No assets or prices tables.** `investments.market_value` is stored at snapshot time, coupling valuation to snapshot timing. We can't revalue historical positions, can't aggregate cross-account by asset, and have no FX history.
4. **Trades are invisible.** A buy/sell doesn't produce a `transactions` row — it appears as a delta between two `investments` snapshots. Trades can't be categorized, can't be seen in the transaction list, can't participate in budgets or AI context.
5. **`accounts.balance` is denormalized and ambiguous for brokerages** — conflates cash sleeve and sum of position market values into one number, kept honest only by Plaid sync.

The owner reframed the model in a v6 design discussion (May 2026):

1. Investment accounts hold multiple assets in one account.
2. Trades are transactions, just in different units.
3. Bank accounts are accounts with one asset (their denomination currency).
4. Cost basis is desirable but tax-lot precision is optional.
5. **Asset-level quantities are deterministic facts. Account-level values require conversion to the primary currency.**
6. Aggregates may be cached but never stored as facts — recompute from prices on demand.

This ADR locks the schema that follows from those principles.

## Decision

Introduce a unified position-based model. Quantities are facts; valuations are derived.

### 1. Three new tables

**`assets`** — every unit of value is a row here. Fiat currencies, equities, funds, crypto, bonds.

```sql
id                      BIGSERIAL PK
symbol                  TEXT NOT NULL              -- 'USD', 'EUR', 'AAPL', 'BTC', 'VTSAX'
kind                    TEXT NOT NULL CHECK (kind IN ('fiat','equity','fund','crypto','bond','commodity','other'))
display_name            TEXT
quote_currency_asset_id BIGINT NULL REFERENCES assets(id)  -- the fiat this asset is priced in; NULL for fiat itself
precision               SMALLINT NOT NULL DEFAULT 8        -- display precision (USD=2, BTC=8, AAPL=4)
created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
UNIQUE (symbol, kind)
```

Cash currencies (USD, EUR, JPY) are seeded rows. Crypto-as-money (BTC, ETH) too. Securities are inserted on first encounter (during Plaid sync or manual entry).

**`positions`** — current (account × asset) snapshot. Replaces `accounts.balance` for cash and `investments.{quantity,market_value}` for holdings.

```sql
id          BIGSERIAL PK
user_id     BIGINT NOT NULL REFERENCES users(id)
account_id  BIGINT NOT NULL REFERENCES accounts(id)
asset_id    BIGINT NOT NULL REFERENCES assets(id)
quantity    NUMERIC(30, 18) NOT NULL              -- FACT — never derived
cost_basis  NUMERIC(30, 18) NULL                  -- average-cost in user's primary currency; null = unknown
updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
deleted_at  TIMESTAMPTZ
UNIQUE (account_id, asset_id) WHERE deleted_at IS NULL
INDEX (user_id, account_id) WHERE deleted_at IS NULL
```

A bank account has exactly one row (asset_id = its denomination). A brokerage has many — one per holding plus one for the cash sleeve.

**`prices`** — append-only price/FX time series.

```sql
id             BIGSERIAL PK
asset_id       BIGINT NOT NULL REFERENCES assets(id)
quote_asset_id BIGINT NOT NULL REFERENCES assets(id)   -- always a fiat asset
as_of          TIMESTAMPTZ NOT NULL
price          NUMERIC(30, 18) NOT NULL
source         TEXT NOT NULL                            -- 'plaid' | 'yahoo' | 'ecb' | 'manual'
created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
INDEX (asset_id, quote_asset_id, as_of DESC)
```

FX: `(EUR, USD, 2026-05-24, 1.08, 'ecb')`. Securities: `(AAPL, USD, 2026-05-24T16:00Z, 189.42, 'plaid')`. "Latest-price-as-of-T in quote_asset" is a single indexed lookup.

### 2. Changes to existing tables

**`accounts`:**
- Drop `balance` (was a denormalization; now strictly derived from positions × prices).
- Add `primary_quote_asset_id BIGINT NOT NULL REFERENCES assets(id)` — the asset the user wants account-level rollups expressed in. Usually equals `users.primary_currency_asset_id` but can differ (e.g. a euro-domiciled account naturally rolls up in EUR).
- `account_type` shrinks in meaning: now a display hint ("brokerage UI vs checking UI"), not a data-shape switch. Schema-level shape is uniform — every account is a bag of positions.

**`transactions`:**
- Add `asset_id BIGINT NOT NULL REFERENCES assets(id)` — what's being moved.
- Reinterpret `amount` as "quantity of `asset_id`" (positive=in, negative=out).
- `currency` becomes derivable from `asset_id` and is dropped in a later phase (after backfill).
- Trades are represented as **two paired rows** linked by the existing `transfer_pair_id`. Example: buying 10 AAPL for $1,820 cash in a brokerage produces:
  - `(account=brokerage, asset_id=USD, amount=-1820.00, transfer_pair_id=X)`
  - `(account=brokerage, asset_id=AAPL, amount=+10, transfer_pair_id=X)`

**`users`:**
- Add `primary_currency_asset_id BIGINT NOT NULL REFERENCES assets(id)` — drives net worth, dashboard totals, AI context. Default USD; user-configurable.

### 3. Trades come from imports — the app never invents transactions

**Invariant:** the application never synthesizes transaction rows to explain a state delta. If Plaid returns a holdings snapshot that disagrees with our recorded positions, we update positions; we **do not** insert plausible trade rows to bridge the gap. This preserves the audit property that every transaction row corresponds to a real event in a real statement.

Consequences:
- Trade-pair generation is the import source's responsibility. Plaid's investment-transactions API already returns paired buy/sell events; CSV ingesters parse trades from statement rows; manual entry requires the user to enter both legs (or use a "record a trade" form that synthesizes the pair *from user input*, which is the user inventing them, not the app).
- Cost basis recomputation walks `transactions` where `asset_id != cash_sleeve_asset`. When trade history is absent (Plaid holdings-only mode, partial manual entry), `positions.cost_basis` may be null or import-provided. Null is honest; we don't backfill from nowhere.

### 4. Cost basis: average cost, stored in primary currency

Tax-lot precision (FIFO/LIFO/specific identification) is deferred. For now:

- `positions.cost_basis` is the running total cost basis using the **average-cost** method, denominated in the user's `primary_currency_asset_id`.
- On a buy: `cost_basis += price × qty × fx_to_primary` (using trade-date FX from `prices`).
- On a sell: `cost_basis -= (cost_basis / quantity_before) × qty_sold` (proportional reduction).
- On corporate actions (splits, mergers): manual adjustment, no automation.

Promotion path: a future `tax_lots` table supersedes `positions.cost_basis` for users who opt in. Single-flag in user settings. Not blocking.

Why primary currency, not trade currency: it's what the user sees on the unrealized-G/L line, and it matches IRS treatment (basis in reporting currency at trade-date FX). The deterministic-quantity / derived-valuation split (constraint #5) is preserved — only `quantity` is a per-trade fact; `cost_basis` is derived at trade time and cached on the position.

### 5. Prices: three tiers, build the first two now

- **Tier 1 — manual entry.** User types prices on a position. Always available, offline, no API key.
- **Tier 2 — Plaid-fed.** Back-compute price from Plaid investment-holdings `(institution_price, quantity)` on each sync; persist into `prices`. Already implicit in `investments.market_value` today.
- **Tier 3 — live feed.** Pluggable `PriceProvider` interface (Yahoo / Polygon / ECB / openexchangerates / CoinGecko). Deferred to a separate milestone.

Tier 1+2 ship with this refactor. Tier 3 lands later behind a single interface, mirroring the `AIProvider` pattern.

### 6. Aggregates are cached, never stored as facts

Per constraint #6, computed values (account balance in primary currency, household net worth, allocation percentages) live in **caches**, not fact tables. Implementation choices the schema doesn't constrain:

- In-memory caches at the service layer keyed by `(scope, period, asset, as_of)`.
- A future `account_value_cache` table if invalidation gets tricky.
- Aggregator output is already non-stored (computed on every request); this ADR doesn't change that.

The invariant: `positions` + `prices` are the only sources of truth. Drop the cache, recompute everything.

## Migration Plan

Phased to keep the system runnable between phases.

**Phase 1 — Foundation tables, backfill, dual-write.**
- Migration creates `assets`, `positions`, `prices`. Seeds common fiat assets (USD, EUR, GBP, JPY, CNY, CAD, AUD) and crypto (BTC, ETH).
- Add `users.primary_currency_asset_id` (default USD asset id).
- Backfill `positions` from existing `accounts.balance` (one row per cash account) and from the latest `investments` snapshot per `(account_id, ticker)`.
- Backfill `prices` from historical `investments.market_value / quantity` rows.
- Old columns (`accounts.balance`, `investments.market_value`) **stay populated** by a dual-write layer.

**Phase 2 — Reads migrate to positions/prices.**
- Investment service `PortfolioSummary` reads from positions + prices.
- Dashboard service net-worth/balance reads from positions + prices (with FX where needed).
- Plaid sync writes positions in addition to `accounts.balance` and `investments`.
- Household aggregator gains `Allocation`, `NetWorthTrend`, `AccountSummaries` methods using the new tables (closes the M9 #225 gap).

**Phase 3 — Trades and `transactions.asset_id`.**
- Migration adds `transactions.asset_id NOT NULL` (backfilled from `accounts.currency` for existing rows — all current transactions are cash transactions, so `asset_id = currency asset of the parent account`).
- Plaid investment-transactions API ingestion: writes paired-row trades.
- CSV/manual trade entry forms produce paired rows.
- Cost-basis recomputation job populates `positions.cost_basis`.

**Phase 4 — Drop legacy columns.**
- Drop `accounts.balance`, `investments.market_value`, `transactions.currency`.
- `investments` table may remain as the historical snapshot log or be retired in favor of `prices` + `positions` — decide at the end of Phase 3.

Each phase is a separate PR. The system runs the full product surface after each phase; rollback is a single migration `down`.

## Consequences

- `accounts` becomes uniform in shape — `account_type` is decorative. Adding new account types (HSA, escrow, retirement-by-tax-treatment) becomes trivial.
- Multi-currency accounts work naturally.
- Brokerage cash sleeves stop being a special case.
- Trades become first-class transactions; budgets, categorization rules, AI context, and household aggregation all see them.
- Allocation, net worth, unrealized G/L become deterministic functions of `(positions, prices, as_of)`. Bug surface drops because there's no second source of truth to keep in sync.
- Cost is real: 4 phased migrations, sync rewrites for Plaid, frontend refactors for the dashboard and investments pages, an FX layer at the price service. Estimated effort: a full milestone (M10).
- Tier 3 price providers, tax-lot precision, and FX-historical revaluation are deferred but unblocked by the schema.

## Alternatives Considered

- **Keep the two-shape model and add a `household_investments` aggregator method.** Cheapest. Solves M9 #225 without a refactor. Rejected because it leaves every other gap (multi-currency, cash sleeves, trade visibility, FX) unaddressed — paying for the refactor later is strictly more expensive than paying for it now once the rationale is locked.
- **One-row trades with a `consideration_asset_id` + `consideration_amount` column pair.** Rejected — half-built double-entry. Balance check is implicit, partial fills awkward, DRIP weird, transfer-pair semantics inconsistent across rows. The existing `transfer_pair_id` already supports the clean two-row form; adding consideration columns is a new foot-gun for no win.
- **App synthesizes trade rows from holdings deltas.** Rejected — violates the "app never invents transactions" invariant. Audit trail must reflect real events; "we inferred a sell because the share count went down" is a guess masquerading as fact.
- **Tax-lot tracking from day one.** Rejected for now. Average cost is sufficient for personal-finance accuracy; tax-lot precision is a separate opt-in user concern (and a separate UI). The promotion path (`tax_lots` table superseding `positions.cost_basis`) doesn't require schema changes here.
- **Store `cost_basis` in the trade currency, convert on display.** Rejected — every read path would need FX at render time, and the user's reporting currency view (the one that matches their tax return) would always be a derived value with rounding drift. Storing in primary currency at trade-time FX matches tax semantics and keeps reads simple.

## Follow-up

- **ADR-0014: Price provider interface (Tier 3).** Pluggable `PriceProvider` covering Yahoo/Polygon for equities, ECB/openexchangerates for FX, CoinGecko for crypto. Mirrors `AIProvider` pattern. Defer until at least one non-Plaid user asks for live prices.
- **ADR-0015: Tax-lot precision opt-in.** Defines `tax_lots` table, lot-selection method (FIFO/LIFO/spec ID), and the migration path from `positions.cost_basis` average-cost to tax-lot precision. Defer until a user needs it.
- **Cash sleeve in brokerage Plaid sync.** Plaid's investment-holdings endpoint reports cash as a position with `security.ticker_symbol = 'CUR:USD'` or similar. The Phase 2 sync rewrite must handle this consistently with bank-account cash, not as a separate code path.
