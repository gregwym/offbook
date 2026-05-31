BEGIN;

ALTER TABLE transactions DROP CONSTRAINT transactions_source_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_source_check
    CHECK (source IN ('plaid', 'csv', 'pdf', 'manual'));

DROP TABLE IF EXISTS account_balance_observations;

COMMIT;
