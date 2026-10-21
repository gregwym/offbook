-- #553: a rule can keep each matched row's category and only rename, tag,
-- hide or mark reviewed. category_id NULL means "keep current category";
-- the service rejects a rule with no category and no other action.
-- Additive: existing rules all carry a category and behave as before.
ALTER TABLE categorization_rules
    ALTER COLUMN category_id DROP NOT NULL;
