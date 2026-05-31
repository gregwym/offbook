BEGIN;

-- #285: give categories an owner so user-created categories can't collide in
-- a global namespace once category CRUD ships. Latent today — categories are
-- read-only and system-seeded (000002) — so this is preventative.
--
-- NULL user_id = system category (the seeded taxonomy); non-null = owned by
-- that user. is_system is retained and stays equivalent to (user_id IS NULL).
ALTER TABLE categories
    ADD COLUMN user_id BIGINT REFERENCES users(id);

-- Slug uniqueness becomes per-owner. COALESCE(user_id, 0) buckets all system
-- categories under owner "0" (real user ids start at 1), so system slugs stay
-- globally unique while each user gets an independent slug namespace.
DROP INDEX IF EXISTS uq_categories_slug;
CREATE UNIQUE INDEX uq_categories_slug
    ON categories (COALESCE(user_id, 0), slug)
    WHERE deleted_at IS NULL;

CREATE INDEX ix_categories_user_id
    ON categories (user_id)
    WHERE deleted_at IS NULL AND user_id IS NOT NULL;

COMMIT;
