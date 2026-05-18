-- #89: scope categorization_rules by user_id.
--
-- `categorization_rules` was created in migration 000001 before the
-- multi-tenant model landed (M2.5). Per .claude/rules/database.md, every
-- user-owned domain table requires `user_id BIGINT NOT NULL REFERENCES users(id)`.
-- The table is unused so far (M4 ships the first writers), so we add the
-- column NOT NULL outright — no backfill needed.
ALTER TABLE categorization_rules
    ADD COLUMN user_id BIGINT NOT NULL REFERENCES users(id);

-- Lookups in the engine always filter by (user_id, is_active=true) ordered
-- by priority DESC, id. Replace the existing priority index with a
-- user-scoped equivalent.
DROP INDEX IF EXISTS ix_categorization_rules_priority;

CREATE INDEX ix_categorization_rules_user_priority
    ON categorization_rules (user_id, priority DESC, id)
    WHERE deleted_at IS NULL AND is_active = TRUE;
