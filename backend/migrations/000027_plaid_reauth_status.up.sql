BEGIN;

-- #364: Plaid re-auth flow.
--
-- ITEM_LOGIN_REQUIRED / PENDING_EXPIRATION sync failures mean the stored
-- access_token's credentials are stale — retrying the same token just
-- fails forever. They get their own status distinct from generic 'error'
-- so the scheduler (#363) skips them without retry-storming and the
-- Settings UI can show a targeted "Reconnect" CTA instead of a dead-end
-- failure message.
ALTER TABLE plaid_items DROP CONSTRAINT IF EXISTS plaid_items_last_sync_status_check;
ALTER TABLE plaid_items
    ADD CONSTRAINT plaid_items_last_sync_status_check
    CHECK (last_sync_status IN ('never','syncing','ok','ok_with_errors','error','reauth_required'));

COMMIT;
