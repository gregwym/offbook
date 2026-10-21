DROP INDEX IF EXISTS idx_categorization_rules_account_id;
ALTER TABLE categorization_rules
    DROP CONSTRAINT IF EXISTS chk_categorization_rules_amount_range,
    DROP COLUMN IF EXISTS amount_max,
    DROP COLUMN IF EXISTS amount_min,
    DROP COLUMN IF EXISTS account_id;
