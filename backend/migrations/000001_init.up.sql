BEGIN;

-- categories: hierarchical, system-seeded + user-added
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

-- accounts: user-labeled financial accounts. PII (holder name, account number) lives in pii_store.
CREATE TABLE accounts (
    id                  BIGSERIAL PRIMARY KEY,
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
CREATE UNIQUE INDEX uq_accounts_plaid_account_id
    ON accounts (plaid_account_id)
    WHERE deleted_at IS NULL AND plaid_account_id IS NOT NULL;
CREATE INDEX ix_accounts_institution_slug ON accounts (institution_slug) WHERE deleted_at IS NULL;
CREATE INDEX ix_accounts_plaid_item_id ON accounts (plaid_item_id) WHERE deleted_at IS NULL AND plaid_item_id IS NOT NULL;

-- transactions: source of truth for all line items. transfer_pair_id links matched transfers.
CREATE TABLE transactions (
    id                      BIGSERIAL PRIMARY KEY,
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

-- categorization_rules: ordered by priority; first match wins
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

-- budgets: per-category spend limits per period
CREATE TABLE budgets (
    id          BIGSERIAL PRIMARY KEY,
    category_id BIGINT NOT NULL REFERENCES categories(id),
    period      TEXT NOT NULL CHECK (period IN ('monthly','weekly','annual')),
    amount      NUMERIC(30, 18) NOT NULL,
    rollover    BOOLEAN NOT NULL DEFAULT FALSE,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_budgets_category_period
    ON budgets (category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE;

-- savings_goals: named targets, optionally linked to an account
CREATE TABLE savings_goals (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    target_amount   NUMERIC(30, 18) NOT NULL,
    current_amount  NUMERIC(30, 18) NOT NULL DEFAULT 0,
    target_date     DATE,
    account_id      BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX ix_savings_goals_account_id
    ON savings_goals (account_id)
    WHERE deleted_at IS NULL AND account_id IS NOT NULL;

-- investments: append-only snapshots. NUMERIC(30,18) supports crypto precision.
CREATE TABLE investments (
    id              BIGSERIAL PRIMARY KEY,
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
CREATE INDEX ix_investments_account_snapshot
    ON investments (account_id, snapshot_date DESC);
CREATE INDEX ix_investments_ticker
    ON investments (ticker, snapshot_date DESC);

-- ai_conversations: a chat thread with the AI advisor
CREATE TABLE ai_conversations (
    id          BIGSERIAL PRIMARY KEY,
    title       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

-- ai_messages: individual turns. context_snapshot records the anonymized DB context passed to the LLM.
CREATE TABLE ai_messages (
    id                  BIGSERIAL PRIMARY KEY,
    conversation_id     BIGINT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role                TEXT NOT NULL CHECK (role IN ('user','assistant','system')),
    content             TEXT NOT NULL,
    context_snapshot    JSONB,
    provider            TEXT,
    model_name          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_ai_messages_conversation_created
    ON ai_messages (conversation_id, created_at);

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
