-- Remove only system-seeded rows. User-created categories (is_system = FALSE)
-- are preserved.
DELETE FROM categories
WHERE is_system = TRUE
  AND deleted_at IS NULL;
