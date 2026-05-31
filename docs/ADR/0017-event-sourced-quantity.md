# ADR-0017: Event-Sourced Quantity — Transactions as the Single Source of Quantity

**Status:** Accepted (amends [ADR-0013](0013-position-based-account-model.md))

**Context date:** 2026-05-31

## Context

ADR-0013 established the position model: *quantities are facts, valuations derive from `positions × prices`*. It got the **separation** right (quantity-fact vs. price-derived-value) but made the **current** quantity the fact and stored it in a mutable, current-only table:

- `positions` is `UNIQUE(account_id, asset_id) WHERE deleted_at IS NULL`, overwritten in place. Plaid sync and the manual opening-balance path write `positions.quantity` **imperatively**.
- There is no per-date quantity history. `prices` is a proper time series; `positions` is not.

Consequences (surfaced in the 2026-05-30 architecture review, issue #281):

- **Point-in-time valuation is unsound.** `value(t) = quantity(t) × price(t)` is unrealizable because `quantity(t)` does not exist. The two net-worth-trend implementations cope in opposite, mutually inconsistent ways (one holds price constant and back-derives quantity from transaction amounts — mixing share-counts and currency; the other holds *current* quantity constant and varies only price). They disagree by construction (see #282).
- Even the *current* quantity can't be regenerated, because it's written imperatively rather than derived from facts.

## Decision

**`transactions` is the single source of truth for quantity.** Enforce the invariant:

> `Σ transactions.amount = positions.quantity`, **per `(account, asset)`**, over non-deleted rows.

with `quantity_asof(account, asset, t) = Σ amount WHERE transaction_date ≤ t AND deleted_at IS NULL`. `positions` becomes a **materialized fold (cache)** of the ledger, regenerable at any time. `value(set, t)` is then a pure function of facts: `Σ quantity_asof(t) × price_asof(t)`.

### Transaction `kind` discriminator

`transactions.kind ∈ { flow, trade_leg, opening_balance, adjustment }`:

- **flow** — ordinary cash movement (income, spending, transfer). The only kind counted by spending / cash-flow / budget analytics.
- **trade_leg** — one leg of a paired trade (security or cash leg), linked via `transfer_pair_id` (ADR-0013). Excluded from flow analytics; included in quantity reconstruction.
- **opening_balance** — the day-0 anchor (one row per held asset at account link). `amount = observed_balance_at_link − Σ(imported transactions for that asset)`, so the invariant holds by construction at link time.
- **adjustment** — a dated delta written whenever a later observed balance diverges from the transaction fold (corporate actions; Plaid history-window gaps; fees/interest not surfaced as transactions).

`opening_balance` and `adjustment` are **quantity facts but not flow** — included in net-worth/quantity reconstruction, excluded from spending/cash-flow/budget aggregates.

### Relationship to ADR-0013's "never invent transactions"

ADR-0013 forbade synthesizing transactions "to reconcile a holdings-snapshot delta." This ADR **refines** that: we still never synthesize a **trade** (no fake buys/sells inferred from a share-count change). But explicit, typed, dated, transparent `opening_balance` and `adjustment` rows are legitimate — they record *observed state* (standard double-entry "opening balance equity" + bank-feed reconciliation), not guessed trades. The ban is narrowed from "no synthetic transactions" to "no synthetic **trades**."

### Reconciliation policy: trust-feed + adjustment

When an institution's reported balance diverges from the transaction fold on a later sync:

- **Chosen:** write an explicit dated `adjustment` for the delta (QuickBooks/YNAB style). The displayed balance matches the institution, the invariant is preserved, and the discrepancy is visible and auditable.
- *Rejected — trust-ledger:* show the app's computed balance and surface drift for review. Rejected because a personal-finance app's number disagreeing with the bank's own app is a trust-killer.

The opening balance anchors day 0; it is **necessary but not sufficient** — without ongoing adjustments the invariant re-breaks on the second sync.

### Reconciliation input / audit

Per-sync observed balances/holdings are recorded in an append-only `account_balance_observations` log — the *input* used to compute adjustment deltas and an audit trail. It is **not** the quantity source of truth (the ledger is).

## Rollout

Pre-prod (dev DBs wiped & rebuilt), staged to keep PRs reviewable:

1. **Foundation (#281, this PR):** add `transactions.kind` (default `flow`); classify trade legs as `trade_leg`; this ADR. No behavior change to aggregates yet.
2. **Event-sourcing (follow-up):** `account_balance_observations` table; `opening_balance` on manual + Plaid account link; `positions` becomes the regenerable fold (Plaid sync writes transactions/observations + derives positions instead of overwriting); `adjustment` generation on sync divergence; flow analytics filter to `kind = 'flow'` everywhere (also fixing any trade-leg leakage into spending); a regeneration command + the per-asset invariant test.
3. **Unified valuation (#282):** one `value(set, asOf, quote)` derivation consuming `quantity_asof`; unify personal + household net-worth/allocation/trends; stop coercing missing price → \$0 (surface incomplete). Depends on step 2 for true historical quantity.

## Consequences

- Net worth, allocation, and per-asset value become computable at any point in time from stored facts, and every aggregate becomes a regenerable cache.
- One mechanism (`adjustment`) handles corporate actions, history-window gaps, and reconciliation — no special cases.
- Cost: every position-writing path (Plaid sync, manual trade, opening balance) must write transactions and derive positions; the flow-analytics filter moves from `is_transfer` to `kind`. Concentrated in the follow-up, behind the foundation laid here.
