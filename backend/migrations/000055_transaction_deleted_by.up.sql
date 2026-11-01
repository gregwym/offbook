-- #696: record who soft-deleted a transaction, so Plaid sync can tell a row
-- the user deleted from one Plaid removed. Additive only.
--   deleted_by: 'user' (Transactions page delete), 'sync' (Plaid `removed`),
--               'import_undo' (statement import undone). NULL on live rows and
--               on rows deleted before this column existed.
--   restored_after_user_delete_at: set when sync brings back a row the user
--               deleted (the bank posted or re-sent it); the UI shows a note
--               and the row goes back into Needs review.
ALTER TABLE transactions
    ADD COLUMN deleted_by TEXT NULL
        CONSTRAINT ck_transactions_deleted_by CHECK (deleted_by IN ('user', 'sync', 'import_undo')),
    ADD COLUMN restored_after_user_delete_at TIMESTAMPTZ NULL;
