BEGIN;

-- plaid_items: one row per linked Plaid Item (≈ one financial institution
-- connection per user). Bearer-equivalent access_token is stored encrypted
-- at rest with PLAID_TOKEN_KEY (see ADR-0010).
--
-- Soft-deletable so revoke/disconnect flows can keep historical context
-- without rehydrating bearer tokens.
CREATE TABLE plaid_items (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id),
    plaid_item_id       TEXT NOT NULL,
    access_token_enc    BYTEA NOT NULL,
    institution_id      TEXT,
    institution_name    TEXT,
    status              TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active','login_required','disconnected','error')),
    cursor              TEXT,
    last_synced_at      TIMESTAMPTZ,
    last_error          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

-- One live row per Plaid Item id, even across users (Plaid item IDs are
-- globally unique). Partial WHERE deleted_at IS NULL so re-linking after
-- a disconnect doesn't violate uniqueness.
CREATE UNIQUE INDEX uq_plaid_items_item_id
    ON plaid_items (plaid_item_id)
    WHERE deleted_at IS NULL;

CREATE INDEX ix_plaid_items_user_id
    ON plaid_items (user_id)
    WHERE deleted_at IS NULL;

COMMIT;
