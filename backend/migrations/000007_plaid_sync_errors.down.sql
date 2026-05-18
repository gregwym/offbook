BEGIN;

-- Restore the narrower status enum first; any rows currently sitting at
-- 'ok_with_errors' get coerced to 'ok' so the new CHECK can apply.
UPDATE plaid_items SET last_sync_status = 'ok' WHERE last_sync_status = 'ok_with_errors';

ALTER TABLE plaid_items DROP CONSTRAINT IF EXISTS plaid_items_last_sync_status_check;
ALTER TABLE plaid_items
    ADD CONSTRAINT plaid_items_last_sync_status_check
    CHECK (last_sync_status IN ('never','syncing','ok','error'));

DROP INDEX IF EXISTS ix_plaid_sync_errors_user;
DROP INDEX IF EXISTS ix_plaid_sync_errors_item_unresolved;
DROP TABLE IF EXISTS plaid_sync_errors;

COMMIT;
