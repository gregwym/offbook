-- Reverse 000008: restore the global priority index, drop the user-scoped
-- one, then drop user_id. Order matters — drop the index that depends on
-- the column before the column itself.
DROP INDEX IF EXISTS ix_categorization_rules_user_priority;

CREATE INDEX ix_categorization_rules_priority
    ON categorization_rules (priority DESC, id)
    WHERE deleted_at IS NULL AND is_active = TRUE;

ALTER TABLE categorization_rules DROP COLUMN IF EXISTS user_id;
