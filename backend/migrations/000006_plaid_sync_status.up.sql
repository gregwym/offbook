BEGIN;

-- #65: per-item sync status indicator.
--
-- Renames the existing `last_error` column to the more descriptive
-- `last_sync_error` and adds a coarse status enum so the UI can render a
-- traffic-light pill without having to peek at the text. `last_synced_at`
-- already exists from 000003.
--
-- Additive: existing rows get `last_sync_status='never'` until the next
-- sync flips them to 'syncing' / 'ok' / 'error'. Renames are a metadata
-- change in Postgres; no rewrite, no lock escalation worth worrying about
-- at this table's size.
ALTER TABLE plaid_items RENAME COLUMN last_error TO last_sync_error;

ALTER TABLE plaid_items
    ADD COLUMN last_sync_status TEXT NOT NULL DEFAULT 'never'
        CHECK (last_sync_status IN ('never','syncing','ok','error'));

COMMIT;
