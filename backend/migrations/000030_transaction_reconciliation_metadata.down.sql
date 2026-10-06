-- #370 down: drops the reconciliation-linkage/acknowledgment metadata added
-- in the up migration. Per the Migration Safety policy, this is a dev/
-- rollback tool, not a data-recovery tool — any backfilled links or
-- acknowledgments are lost, not restored by a later up.
DROP INDEX IF EXISTS idx_transactions_caused_by_observation;

ALTER TABLE transactions
    DROP COLUMN IF EXISTS acknowledged_note,
    DROP COLUMN IF EXISTS acknowledged_at,
    DROP COLUMN IF EXISTS caused_by_observation_id;
