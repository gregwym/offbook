DROP INDEX IF EXISTS ix_categorization_rules_asset_id;
ALTER TABLE categorization_rules DROP COLUMN IF EXISTS asset_id;
