-- #338 Phase 3 (ADR-0014 §3): scheduled price refresh is background egress
-- (held-symbol lists leave the box without a user action), so it requires a
-- persistent per-user opt-in — unlike the manual refresh button, where the
-- click itself is the consent. Default OFF.
ALTER TABLE user_settings
    ADD COLUMN auto_price_refresh BOOLEAN NOT NULL DEFAULT FALSE;
