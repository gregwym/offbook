-- #131: per-user AI provider settings.
--
-- API keys are user-scoped (multiple users on the same instance keep
-- separate keys). The Claude key is stored encrypted at rest using the
-- existing crypto.SecretBox AES-256-GCM scheme — same primitive as
-- Plaid access tokens (ADR-0010). The encryption key is derived from
-- SESSION_SECRET via SHA-256 so there's no new operator-facing secret
-- to manage.
--
-- One row per user. The router auto-creates a row on first /me/settings
-- access, so handlers can assume the row exists for an authenticated
-- session.
CREATE TABLE user_settings (
    user_id            BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- Encrypted ciphertext. NULL → user has not configured a key.
    claude_api_key_enc BYTEA NULL,
    -- Local Ollama daemon URL. NULL → falls back to OLLAMA_BASE_URL env.
    ollama_base_url    TEXT NULL,
    -- Which provider the AI service uses for this user's SendMessage.
    -- 'claude' is the default since most users will start with a key.
    preferred_provider TEXT NOT NULL DEFAULT 'claude' CHECK (preferred_provider IN ('claude', 'ollama')),
    -- Optional model override. NULL → use provider default (claude-sonnet-4-6
    -- or llama3:8b). Free-form because Ollama users can install anything.
    preferred_model    TEXT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
