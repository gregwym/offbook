DROP INDEX IF EXISTS uq_transactions_user_plaid;

CREATE UNIQUE INDEX uq_transactions_plaid
    ON transactions (plaid_transaction_id)
    WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL;
