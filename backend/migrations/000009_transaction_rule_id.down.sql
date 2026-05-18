-- Reverse 000009: drop the partial index then the column.
DROP INDEX IF EXISTS ix_transactions_categorization_rule_id;

ALTER TABLE transactions DROP COLUMN IF EXISTS categorization_rule_id;
