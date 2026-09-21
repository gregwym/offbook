BEGIN;

-- Restore the narrower status enum first; any rows currently sitting at
-- 'reauth_required' get coerced to 'error' so the new CHECK can apply.
UPDATE plaid_items SET last_sync_status = 'error' WHERE last_sync_status = 'reauth_required';

ALTER TABLE plaid_items DROP CONSTRAINT IF EXISTS plaid_items_last_sync_status_check;
ALTER TABLE plaid_items
    ADD CONSTRAINT plaid_items_last_sync_status_check
    CHECK (last_sync_status IN ('never','syncing','ok','ok_with_errors','error'));

COMMIT;
