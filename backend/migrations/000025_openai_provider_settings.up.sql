-- #354: OpenAI-compatible AI provider. Two new per-user settings so the AI
-- advisor can target any OpenAI-compatible chat-completions endpoint (real
-- OpenAI, or a local proxy fronting a Claude Max / ChatGPT subscription):
--   - openai_base_url: the "/v1" root to POST against (NULL → public OpenAI).
--   - openai_api_key_enc: bearer token, encrypted at rest via the same
--     SecretBox as claude_api_key_enc; NULL when the endpoint needs no key.
ALTER TABLE user_settings
    ADD COLUMN openai_base_url    TEXT,
    ADD COLUMN openai_api_key_enc BYTEA;

-- Widen the preferred_provider CHECK to admit 'openai' (was claude|ollama,
-- from migration 000010).
ALTER TABLE user_settings
    DROP CONSTRAINT user_settings_preferred_provider_check;
ALTER TABLE user_settings
    ADD CONSTRAINT user_settings_preferred_provider_check
        CHECK (preferred_provider IN ('claude', 'ollama', 'openai'));
