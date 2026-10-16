-- #424 down: drops the review-state column. Dev/rollback tool only — reviewed
-- state is lost, not restored by a later up (which re-runs the backfill).
DROP INDEX IF EXISTS idx_transactions_unreviewed;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS reviewed_at;
