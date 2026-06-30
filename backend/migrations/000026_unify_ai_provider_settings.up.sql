-- #354 follow-up: collapse the per-provider AI settings into one generic
-- triplet — protocol (preferred_provider), endpoint, token — so the Settings
-- UI is a single "pick a protocol, point it at an endpoint, give it a token"
-- form instead of one block per provider. The protocol column keeps its name
-- (preferred_provider) and its claude|ollama|openai CHECK from migration 000025.
ALTER TABLE user_settings
    ADD COLUMN api_endpoint  TEXT,
    ADD COLUMN api_token_enc BYTEA;

-- Carry the existing per-provider values over to the unified columns based on
-- the active protocol. Claude/OpenAI tokens are both SecretBox ciphertext, so
-- copying the bytes preserves decryptability.
UPDATE user_settings SET
    api_endpoint = CASE preferred_provider
        WHEN 'ollama' THEN ollama_base_url
        WHEN 'openai' THEN openai_base_url
        ELSE NULL
    END,
    api_token_enc = CASE preferred_provider
        WHEN 'openai' THEN openai_api_key_enc
        WHEN 'claude' THEN claude_api_key_enc
        ELSE NULL
    END;

ALTER TABLE user_settings
    DROP COLUMN claude_api_key_enc,
    DROP COLUMN ollama_base_url,
    DROP COLUMN openai_base_url,
    DROP COLUMN openai_api_key_enc;
