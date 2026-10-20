DROP INDEX IF EXISTS idx_transactions_tags;
ALTER TABLE transactions DROP COLUMN IF EXISTS tags;
