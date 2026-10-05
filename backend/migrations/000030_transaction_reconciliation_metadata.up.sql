-- #370: Reconciliation view per account + unexplained-delta flag.
--
-- caused_by_observation_id ties an opening_balance/adjustment row to the
-- account_balance_observations row that caused it to be written (ADR-0017
-- ReconcilePosition inserts both in the same operation). Audit linkage only
-- — the reconciliation view's "fold-vs-reported" math is derived independently
-- from the ledger (see FoldQuantityBefore), so a NULL link on legacy rows
-- degrades the UI's traceability but never breaks the flag calculation.
--
-- acknowledged_at / acknowledged_note let a user mark an adjustment reviewed
-- without altering ledger fields (amount/kind/quantity facts stay untouched —
-- this is metadata, same category as the existing `notes` column).
ALTER TABLE transactions
    ADD COLUMN caused_by_observation_id BIGINT NULL REFERENCES account_balance_observations(id) ON DELETE SET NULL,
    ADD COLUMN acknowledged_at TIMESTAMPTZ NULL,
    ADD COLUMN acknowledged_note TEXT NULL;

CREATE INDEX idx_transactions_caused_by_observation
    ON transactions (caused_by_observation_id)
    WHERE caused_by_observation_id IS NOT NULL;

-- Best-effort backfill for rows written before this migration: link an
-- opening_balance/adjustment row to the observation sharing its
-- (account, asset, as_of date) when that match is unambiguous. Rows that
-- don't match (e.g. two observations landed on the same date) stay NULL —
-- the flag math doesn't depend on this link, only the audit-trail display does.
UPDATE transactions t
SET caused_by_observation_id = sub.obs_id
FROM (
    SELECT o.id AS obs_id, o.account_id, o.asset_id, o.as_of
    FROM account_balance_observations o
    WHERE (o.account_id, o.asset_id, o.as_of) IN (
        SELECT account_id, asset_id, as_of
        FROM account_balance_observations
        GROUP BY account_id, asset_id, as_of
        HAVING COUNT(*) = 1
    )
) sub
WHERE t.account_id = sub.account_id
  AND t.asset_id = sub.asset_id
  AND t.transaction_date = sub.as_of
  AND t.kind IN ('opening_balance', 'adjustment')
  AND t.caused_by_observation_id IS NULL;
