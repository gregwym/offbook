-- #366: AI transaction auto-categorization (ADR-0022).
-- auto_categorize is the stored-consent opt-in, same shape as
-- auto_price_refresh (ADR-0014 §3): background egress of merchant strings
-- to the user's configured AI provider needs an explicit click, default off.
ALTER TABLE user_settings
    ADD COLUMN auto_categorize BOOLEAN NOT NULL DEFAULT FALSE;

-- ai_categorization_verdicts: one row per (user, merchant) — the AI verdict
-- cache ADR-0022 §6 describes. Upserted on every AI call so a recurring
-- merchant (subscription, regular grocery run) is priced once, not once per
-- transaction. Also the source list for the frontend's "promote to rule"
-- affordance. Not a domain financial table (no deleted_at) and not an
-- append-only audit trail either (no purge job) — it's a bounded-size cache,
-- one row per distinct merchant per user, upserted in place.
CREATE TABLE ai_categorization_verdicts (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id),
    merchant_key   TEXT NOT NULL,
    category_id    BIGINT NOT NULL REFERENCES categories(id),
    confidence     NUMERIC(5, 4) NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    provider       TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uq_ai_categorization_verdicts_user_merchant
    ON ai_categorization_verdicts (user_id, merchant_key);
