BEGIN;

-- users: auth principal. Email is case-insensitive unique.
-- Must precede every domain table that carries user_id FK.
CREATE TABLE users (
    id             BIGSERIAL PRIMARY KEY,
    email          TEXT NOT NULL,
    password_hash  TEXT NOT NULL,
    is_admin       BOOLEAN NOT NULL DEFAULT FALSE,
    last_scope     TEXT NOT NULL DEFAULT 'personal' CHECK (last_scope IN ('personal','household')),
    default_scope  TEXT NOT NULL DEFAULT 'personal' CHECK (default_scope IN ('personal','household')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_users_email ON users (LOWER(email)) WHERE deleted_at IS NULL;

-- sessions: cookie-backed. token_hash is HMAC-SHA256(token, SESSION_SECRET).
-- The raw token only ever lives in the cookie.
CREATE TABLE sessions (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_sessions_token_hash ON sessions (token_hash);
CREATE INDEX ix_sessions_user_id    ON sessions (user_id);
CREATE INDEX ix_sessions_expires_at ON sessions (expires_at);

-- instance_config: singleton (id=1). Set once during /setup/admin.
CREATE TABLE instance_config (
    id           SMALLINT PRIMARY KEY CHECK (id = 1),
    signup_mode  TEXT NOT NULL CHECK (signup_mode IN ('local_multi_tenant','invite_only')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- households: a shared book. grace_period_days is owner-configurable.
CREATE TABLE households (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL,
    owner_id           BIGINT NOT NULL REFERENCES users(id),
    grace_period_days  INTEGER NOT NULL DEFAULT 30 CHECK (grace_period_days >= 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);
CREATE INDEX ix_households_owner_id ON households (owner_id) WHERE deleted_at IS NULL;

-- household_members: lifecycle via left_at + purged_at (ADR-0007).
CREATE TABLE household_members (
    id            BIGSERIAL PRIMARY KEY,
    household_id  BIGINT NOT NULL REFERENCES households(id),
    user_id       BIGINT NOT NULL REFERENCES users(id),
    role          TEXT NOT NULL CHECK (role IN ('owner','contributor','view_only')),
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at       TIMESTAMPTZ,
    purged_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_household_members_active
    ON household_members (household_id, user_id)
    WHERE purged_at IS NULL;
CREATE INDEX ix_household_members_user_id
    ON household_members (user_id)
    WHERE purged_at IS NULL;

-- household_invites: token-based. token_hash mirrors sessions.
CREATE TABLE household_invites (
    id            BIGSERIAL PRIMARY KEY,
    household_id  BIGINT NOT NULL REFERENCES households(id),
    token_hash    TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('owner','contributor','view_only')),
    created_by    BIGINT NOT NULL REFERENCES users(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    accepted_at   TIMESTAMPTZ,
    accepted_by   BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_household_invites_token_hash ON household_invites (token_hash);
CREATE INDEX ix_household_invites_household ON household_invites (household_id);

-- categories: hierarchical, system-seeded + user-added.
CREATE TABLE categories (
    id          BIGSERIAL PRIMARY KEY,
    parent_id   BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    icon        TEXT,
    color       TEXT,
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_categories_slug ON categories (slug) WHERE deleted_at IS NULL;
CREATE INDEX ix_categories_parent_id ON categories (parent_id) WHERE deleted_at IS NULL;

-- accounts: user-labeled financial accounts. PII (holder name, account number)
-- lives in pii_store.
CREATE TABLE accounts (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id),
    name                TEXT NOT NULL,
    institution_slug    TEXT NOT NULL,
    account_type        TEXT NOT NULL CHECK (account_type IN ('checking','savings','credit_card','loan','investment','crypto','cash','other')),
    currency            TEXT NOT NULL DEFAULT 'USD',
    balance             NUMERIC(30, 18) NOT NULL DEFAULT 0,
    last_four           TEXT,
    plaid_account_id    TEXT,
    plaid_item_id       TEXT,
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX ix_accounts_user_id ON accounts (user_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_accounts_plaid_account_id
    ON accounts (plaid_account_id)
    WHERE deleted_at IS NULL AND plaid_account_id IS NOT NULL;
CREATE INDEX ix_accounts_institution_slug ON accounts (institution_slug) WHERE deleted_at IS NULL;
CREATE INDEX ix_accounts_plaid_item_id ON accounts (plaid_item_id) WHERE deleted_at IS NULL AND plaid_item_id IS NOT NULL;

-- transactions: source of truth for all line items.
CREATE TABLE transactions (
    id                      BIGSERIAL PRIMARY KEY,
    user_id                 BIGINT NOT NULL REFERENCES users(id),
    account_id              BIGINT NOT NULL REFERENCES accounts(id),
    category_id             BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    amount                  NUMERIC(30, 18) NOT NULL,
    currency                TEXT NOT NULL DEFAULT 'USD',
    description             TEXT,
    description_clean       TEXT,
    merchant_name           TEXT,
    transaction_date        DATE NOT NULL,
    posted_date             DATE,
    source                  TEXT NOT NULL CHECK (source IN ('plaid','csv','pdf','manual')),
    external_id             TEXT,
    plaid_transaction_id    TEXT,
    categorization_method   TEXT,
    is_transfer             BOOLEAN NOT NULL DEFAULT FALSE,
    transfer_pair_id        BIGINT REFERENCES transactions(id) ON DELETE SET NULL,
    notes                   TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at              TIMESTAMPTZ
);
CREATE INDEX ix_transactions_user_id ON transactions (user_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_transactions_external
    ON transactions (account_id, external_id)
    WHERE deleted_at IS NULL AND external_id IS NOT NULL;
CREATE UNIQUE INDEX uq_transactions_plaid
    ON transactions (plaid_transaction_id)
    WHERE deleted_at IS NULL AND plaid_transaction_id IS NOT NULL;
CREATE INDEX ix_transactions_account_date
    ON transactions (account_id, transaction_date DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX ix_transactions_category_id
    ON transactions (category_id)
    WHERE deleted_at IS NULL AND category_id IS NOT NULL;
CREATE INDEX ix_transactions_transfer_pair_id
    ON transactions (transfer_pair_id)
    WHERE deleted_at IS NULL AND transfer_pair_id IS NOT NULL;

-- account_shares: per-account visibility into a household. Absence = 'private'.
CREATE TABLE account_shares (
    id            BIGSERIAL PRIMARY KEY,
    account_id    BIGINT NOT NULL REFERENCES accounts(id),
    household_id  BIGINT NOT NULL REFERENCES households(id),
    visibility    TEXT NOT NULL CHECK (visibility IN ('balance_only','balance_and_txns')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_account_shares
    ON account_shares (account_id, household_id)
    WHERE deleted_at IS NULL;
CREATE INDEX ix_account_shares_household
    ON account_shares (household_id)
    WHERE deleted_at IS NULL;

-- categorization_rules: ordered by priority; first match wins.
CREATE TABLE categorization_rules (
    id          BIGSERIAL PRIMARY KEY,
    pattern     TEXT NOT NULL,
    category_id BIGINT NOT NULL REFERENCES categories(id),
    match_type  TEXT NOT NULL CHECK (match_type IN ('contains','regex','exact')),
    priority    INTEGER NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX ix_categorization_rules_priority
    ON categorization_rules (priority DESC, id)
    WHERE deleted_at IS NULL AND is_active = TRUE;
CREATE INDEX ix_categorization_rules_category_id
    ON categorization_rules (category_id)
    WHERE deleted_at IS NULL;

-- budgets: per-user, per-category spend limits per period.
CREATE TABLE budgets (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    category_id BIGINT NOT NULL REFERENCES categories(id),
    period      TEXT NOT NULL CHECK (period IN ('monthly','weekly','annual')),
    amount      NUMERIC(30, 18) NOT NULL,
    rollover    BOOLEAN NOT NULL DEFAULT FALSE,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX ix_budgets_user_id ON budgets (user_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_budgets_user_category_period
    ON budgets (user_id, category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE;

-- savings_goals: named targets, optionally linked to an account.
CREATE TABLE savings_goals (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    name            TEXT NOT NULL,
    target_amount   NUMERIC(30, 18) NOT NULL,
    current_amount  NUMERIC(30, 18) NOT NULL DEFAULT 0,
    target_date     DATE,
    account_id      BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX ix_savings_goals_user_id ON savings_goals (user_id) WHERE deleted_at IS NULL;
CREATE INDEX ix_savings_goals_account_id
    ON savings_goals (account_id)
    WHERE deleted_at IS NULL AND account_id IS NOT NULL;

-- investments: append-only snapshots. NUMERIC(30,18) supports crypto precision.
CREATE TABLE investments (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    ticker          TEXT NOT NULL,
    name            TEXT,
    asset_class     TEXT,
    quantity        NUMERIC(30, 18) NOT NULL,
    cost_basis      NUMERIC(30, 18),
    market_value    NUMERIC(30, 18),
    snapshot_date   DATE NOT NULL,
    source          TEXT NOT NULL CHECK (source IN ('plaid','csv','manual')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_investments_user_id      ON investments (user_id);
CREATE INDEX ix_investments_account_snapshot
    ON investments (account_id, snapshot_date DESC);
CREATE INDEX ix_investments_ticker
    ON investments (ticker, snapshot_date DESC);

-- shared_budgets / shared_goals: household-scoped counterparts. No CRUD in M2.5.
CREATE TABLE shared_budgets (
    id            BIGSERIAL PRIMARY KEY,
    household_id  BIGINT NOT NULL REFERENCES households(id),
    category_id   BIGINT NOT NULL REFERENCES categories(id),
    period        TEXT NOT NULL CHECK (period IN ('monthly','weekly','annual')),
    amount        NUMERIC(30,18) NOT NULL,
    rollover      BOOLEAN NOT NULL DEFAULT FALSE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_shared_budgets_active
    ON shared_budgets (household_id, category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE;

CREATE TABLE shared_goals (
    id              BIGSERIAL PRIMARY KEY,
    household_id    BIGINT NOT NULL REFERENCES households(id),
    name            TEXT NOT NULL,
    target_amount   NUMERIC(30,18) NOT NULL,
    current_amount  NUMERIC(30,18) NOT NULL DEFAULT 0,
    target_date     DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX ix_shared_goals_household
    ON shared_goals (household_id)
    WHERE deleted_at IS NULL;

-- ai_threads: chat thread with the AI advisor. shared_with_household = true
-- makes a thread visible to other household members via aggregator.AIContext.
CREATE TABLE ai_threads (
    id                     BIGSERIAL PRIMARY KEY,
    user_id                BIGINT NOT NULL REFERENCES users(id),
    household_id           BIGINT REFERENCES households(id),
    shared_with_household  BOOLEAN NOT NULL DEFAULT FALSE,
    title                  TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at             TIMESTAMPTZ
);
CREATE INDEX ix_ai_threads_user_id      ON ai_threads (user_id) WHERE deleted_at IS NULL;
CREATE INDEX ix_ai_threads_household_id ON ai_threads (household_id) WHERE deleted_at IS NULL AND household_id IS NOT NULL;

-- ai_messages: individual turns. context_snapshot records the anonymized DB
-- context passed to the LLM.
CREATE TABLE ai_messages (
    id                  BIGSERIAL PRIMARY KEY,
    thread_id           BIGINT NOT NULL REFERENCES ai_threads(id) ON DELETE CASCADE,
    role                TEXT NOT NULL CHECK (role IN ('user','assistant','system')),
    content             TEXT NOT NULL,
    context_snapshot    JSONB,
    provider            TEXT,
    model_name          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_ai_messages_thread_created ON ai_messages (thread_id, created_at);

-- pii_store: the ONLY table that holds PII. No FK to other tables (PII isolation).
CREATE TABLE pii_store (
    id          BIGSERIAL PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id   BIGINT NOT NULL,
    field_name  TEXT NOT NULL,
    value       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_type, entity_id, field_name)
);

-- ingestion_jobs: tracks CSV/PDF upload jobs. Append-only audit trail.
CREATE TABLE ingestion_jobs (
    id              BIGSERIAL PRIMARY KEY,
    source          TEXT NOT NULL CHECK (source IN ('csv','pdf')),
    account_id      BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    file_name       TEXT,
    status          TEXT NOT NULL CHECK (status IN ('pending','processing','completed','failed')) DEFAULT 'pending',
    rows_total      INTEGER,
    rows_imported   INTEGER,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);
CREATE INDEX ix_ingestion_jobs_status_created
    ON ingestion_jobs (status, created_at DESC);

COMMIT;
