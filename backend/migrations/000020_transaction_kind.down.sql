BEGIN;

DROP INDEX IF EXISTS ix_transactions_kind;
ALTER TABLE transactions DROP COLUMN IF EXISTS kind;

COMMIT;
