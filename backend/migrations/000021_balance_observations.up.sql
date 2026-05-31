BEGIN;

-- #293 (slice 2): reconciliation input + system-generated transaction source.
--
-- account_balance_observations is the append-only audit/reconciliation input
-- (ADR-0017): each row records what a sync source reported for an
-- (account, asset) at a point in time. It is NOT the quantity source of
-- truth — the transaction ledger is. ReconcilePosition compares the reported
-- value to the transaction fold and writes an opening_balance/adjustment for
-- any delta.
CREATE TABLE account_balance_observations (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id),
    account_id        BIGINT NOT NULL REFERENCES accounts(id),
    asset_id          BIGINT NOT NULL REFERENCES assets(id),
    observed_quantity NUMERIC(30, 18) NOT NULL,
    as_of             TIMESTAMPTZ NOT NULL,
    source            TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_abo_account_asset_asof
    ON account_balance_observations (account_id, asset_id, as_of DESC);

-- Allow 'system' as a transaction source for reconciliation rows
-- (opening_balance / adjustment) that the app writes itself — distinct from
-- real Plaid/CSV/manual events. ADR-0017: never invent trades; reconciliation
-- rows are explicit, typed, transparent facts.
ALTER TABLE transactions DROP CONSTRAINT transactions_source_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_source_check
    CHECK (source IN ('plaid', 'csv', 'pdf', 'manual', 'system'));

COMMIT;
