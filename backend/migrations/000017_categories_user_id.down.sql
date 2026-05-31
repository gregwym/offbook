BEGIN;

-- Reverse #285.
DROP INDEX IF EXISTS ix_categories_user_id;
DROP INDEX IF EXISTS uq_categories_slug;
CREATE UNIQUE INDEX uq_categories_slug ON categories (slug) WHERE deleted_at IS NULL;
ALTER TABLE categories DROP COLUMN IF EXISTS user_id;

COMMIT;
