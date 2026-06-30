-- Revert the CHECK to the pre-#354 claude|ollama set. Any 'openai' rows must
-- be migrated off first or this constraint add fails — acceptable pre-prod.
ALTER TABLE user_settings
    DROP CONSTRAINT user_settings_preferred_provider_check;
ALTER TABLE user_settings
    ADD CONSTRAINT user_settings_preferred_provider_check
        CHECK (preferred_provider IN ('claude', 'ollama'));

ALTER TABLE user_settings
    DROP COLUMN IF EXISTS openai_api_key_enc,
    DROP COLUMN IF EXISTS openai_base_url;
