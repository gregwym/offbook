-- #63: scope the Plaid-dedup partial unique index by user_id.
--
-- Migration 000001 created `uq_transactions_plaid` as a globally unique
-- partial index on `(plaid_transaction_id)`. That is *stricter* than
-- multi-tenant correctness requires and creates a real failure mode:
-- two distinct users connecting to Plaid sandbox (or any environment
-- that can replay the same transaction_id under different items) would
-- collide. The semantic we actually want is "no duplicate Plaid row
-- *for the same user*".
--
-- Replace with a partial unique index on (user_id, plaid_transaction_id).
-- Same partial predicate so soft-deleted rows and rows from non-Plaid
-- sources (NULL plaid_transaction_id) remain non-conflicting.
DROP INDEX IF EXISTS uq_transactions_plaid;

CREATE UNIQUE INDEX uq_transactions_user_plaid
    ON transactions (user_id, plaid_transaction_id)
    WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL;
