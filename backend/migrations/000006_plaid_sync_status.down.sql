BEGIN;

ALTER TABLE plaid_items DROP COLUMN IF EXISTS last_sync_status;
ALTER TABLE plaid_items RENAME COLUMN last_sync_error TO last_error;

COMMIT;
