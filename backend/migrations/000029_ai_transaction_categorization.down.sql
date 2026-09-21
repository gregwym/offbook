DROP TABLE IF EXISTS ai_categorization_verdicts;

ALTER TABLE user_settings
    DROP COLUMN auto_categorize;
