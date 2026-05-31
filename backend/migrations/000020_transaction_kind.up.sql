BEGIN;

-- #281 (foundation): add a `kind` discriminator to transactions.
--
-- Per ADR-0017, transactions become the single source of truth for quantity
-- (positions = Σ amount per (account, asset)). `kind` classifies each row:
--   flow           — ordinary cash movement; the only kind counted by
--                    spending / cash-flow / budget analytics.
--   trade_leg      — one leg of a paired trade (transfer_pair_id).
--   opening_balance— day-0 anchor (one per held asset at account link).
--   adjustment     — dated delta reconciling a later observed balance to the
--                    transaction fold (corporate actions, history gaps, …).
--
-- This migration is additive: default 'flow', no aggregate query changes yet.
-- The event-sourcing behavior (opening_balance/adjustment generation,
-- positions-as-fold, flow-analytics filtering by kind) lands in the follow-up.
ALTER TABLE transactions
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'flow'
    CHECK (kind IN ('flow', 'trade_leg', 'opening_balance', 'adjustment'));

-- Classify existing security/cash legs of trades. A trade leg is a row that
-- participates in a transfer pair AND has a non-fiat asset on at least one
-- side; we mark both legs of any pair containing a non-fiat leg as trade_leg.
-- (Pre-prod: dev DBs are rebuilt, so this only matters for any seeded data.)
WITH trade_pairs AS (
    SELECT t.id, t.transfer_pair_id
    FROM transactions t
    JOIN assets a ON a.id = t.asset_id
    WHERE t.transfer_pair_id IS NOT NULL
      AND a.kind <> 'fiat'
)
UPDATE transactions t
   SET kind = 'trade_leg'
  FROM trade_pairs tp
 WHERE t.id = tp.id
    OR t.id = tp.transfer_pair_id
    OR t.transfer_pair_id = tp.id;

-- Flow analytics will filter on kind; index the common predicate.
CREATE INDEX ix_transactions_kind
    ON transactions (account_id, kind)
    WHERE deleted_at IS NULL;

COMMIT;
