-- Restoring NOT NULL would need a category for every keep-category rule
-- (#553), and picking one would silently change what the rule does. Refuse
-- instead of guessing: delete or edit those rules first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM categorization_rules WHERE category_id IS NULL) THEN
        RAISE EXCEPTION 'categorization_rules has rules without a category (#553); give them a category or delete them before migrating down';
    END IF;
END $$;

ALTER TABLE categorization_rules
    ALTER COLUMN category_id SET NOT NULL;
