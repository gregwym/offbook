-- Restore the per-provider columns (#354) and fan the unified triplet back
-- out by protocol.
ALTER TABLE user_settings
    ADD COLUMN claude_api_key_enc BYTEA,
    ADD COLUMN ollama_base_url    TEXT,
    ADD COLUMN openai_base_url    TEXT,
    ADD COLUMN openai_api_key_enc BYTEA;

UPDATE user_settings SET
    ollama_base_url    = CASE preferred_provider WHEN 'ollama' THEN api_endpoint  ELSE NULL END,
    openai_base_url    = CASE preferred_provider WHEN 'openai' THEN api_endpoint  ELSE NULL END,
    openai_api_key_enc = CASE preferred_provider WHEN 'openai' THEN api_token_enc ELSE NULL END,
    claude_api_key_enc = CASE preferred_provider WHEN 'claude' THEN api_token_enc ELSE NULL END;

ALTER TABLE user_settings
    DROP COLUMN api_token_enc,
    DROP COLUMN api_endpoint;
