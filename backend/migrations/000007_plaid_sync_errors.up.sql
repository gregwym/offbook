BEGIN;

-- #80: per-row DLQ for Plaid /transactions/sync.
--
-- Before this migration, any single bad row in a sync batch rolled back the
-- whole transaction and left the cursor stuck — a 10k-row sync with one bad
-- row lost 9,999 good rows and would replay the failure forever.
--
-- The DLQ row preserves the raw Plaid payload so the owner can retry after
-- a fix (or a schema/precision change) without re-syncing the whole item.
-- Hard-delete only — audit/DLQ table, no soft delete. See docs/ADR/0011.
CREATE TABLE plaid_sync_errors (
    id                   BIGSERIAL PRIMARY KEY,
    user_id              BIGINT NOT NULL REFERENCES users(id),
    plaid_item_id        BIGINT NOT NULL REFERENCES plaid_items(id),
    plaid_transaction_id TEXT,
    raw_payload          JSONB NOT NULL,
    error_code           TEXT NOT NULL,
    error_message        TEXT NOT NULL,
    occurred_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at          TIMESTAMPTZ,
    resolution           TEXT
        CHECK (resolution IS NULL OR resolution IN ('retried_ok','dismissed')),
    CHECK ((resolved_at IS NULL) = (resolution IS NULL))
);

-- Owners want a fast "unresolved errors for this item" query (badge count,
-- modal list). Partial index keeps it tight even as the DLQ grows.
CREATE INDEX ix_plaid_sync_errors_item_unresolved
    ON plaid_sync_errors (plaid_item_id)
    WHERE resolved_at IS NULL;

-- For per-user surfaces (e.g. global DLQ view) and for the multi-tenant
-- read filter — every read MUST scope on user_id.
CREATE INDEX ix_plaid_sync_errors_user
    ON plaid_sync_errors (user_id);

-- Extend the sync status enum so the UI can render partial-success
-- distinct from a clean run.
ALTER TABLE plaid_items DROP CONSTRAINT IF EXISTS plaid_items_last_sync_status_check;
ALTER TABLE plaid_items
    ADD CONSTRAINT plaid_items_last_sync_status_check
    CHECK (last_sync_status IN ('never','syncing','ok','ok_with_errors','error'));

COMMIT;
