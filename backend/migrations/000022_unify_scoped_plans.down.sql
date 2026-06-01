-- Reverse ADR-0018: restore the separate shared_budgets / shared_goals tables
-- and revert budgets / savings_goals to personal-only (user_id NOT NULL).
-- Best-effort: dropping the owner CHECK + restoring NOT NULL only succeeds if
-- no household-owned rows exist (pre-prod, the DB is rebuilt anyway).

-- ── recreate household-only tables (verbatim from 000001) ────────────────
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

-- ── savings_goals: drop unified owner support ────────────────────────────
DROP INDEX ix_savings_goals_household_id;
ALTER TABLE savings_goals DROP CONSTRAINT savings_goals_account_personal_chk;
ALTER TABLE savings_goals DROP CONSTRAINT savings_goals_owner_chk;
DELETE FROM savings_goals WHERE user_id IS NULL;
ALTER TABLE savings_goals ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE savings_goals DROP COLUMN household_id;

-- ── budgets: drop unified owner support ──────────────────────────────────
DROP INDEX ix_budgets_household_id;
DROP INDEX uq_budgets_household_category_period;
DROP INDEX uq_budgets_user_category_period;
CREATE UNIQUE INDEX uq_budgets_user_category_period
    ON budgets (user_id, category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE;
ALTER TABLE budgets DROP CONSTRAINT budgets_owner_chk;
DELETE FROM budgets WHERE user_id IS NULL;
ALTER TABLE budgets ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE budgets DROP COLUMN household_id;
