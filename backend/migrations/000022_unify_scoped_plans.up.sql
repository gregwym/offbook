-- ADR-0018: unify scoped plans. budgets and savings_goals each become a
-- single table owning both personal (user_id) and household (household_id)
-- plans, with exactly one owner set. shared_budgets / shared_goals are
-- dropped — their config collapses into the unified tables. Evaluation stays
-- scope-aware (household plans are still read only via the aggregator).
--
-- Pre-prod: dev/test/qa DBs are wiped & rebuilt, so dropping the shared_*
-- tables discards their rows rather than migrating them.

-- ── budgets ──────────────────────────────────────────────────────────────
ALTER TABLE budgets ADD COLUMN household_id BIGINT REFERENCES households(id);
ALTER TABLE budgets ALTER COLUMN user_id DROP NOT NULL;
-- Exactly one owner: personal (user_id) XOR household (household_id).
ALTER TABLE budgets ADD CONSTRAINT budgets_owner_chk
    CHECK ((user_id IS NOT NULL) <> (household_id IS NOT NULL));

-- Re-scope the personal active-uniqueness to user-owned rows only, and add
-- the matching household-owned uniqueness.
DROP INDEX uq_budgets_user_category_period;
CREATE UNIQUE INDEX uq_budgets_user_category_period
    ON budgets (user_id, category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE AND user_id IS NOT NULL;
CREATE UNIQUE INDEX uq_budgets_household_category_period
    ON budgets (household_id, category_id, period)
    WHERE deleted_at IS NULL AND is_active = TRUE AND household_id IS NOT NULL;
CREATE INDEX ix_budgets_household_id
    ON budgets (household_id)
    WHERE deleted_at IS NULL AND household_id IS NOT NULL;

-- ── savings_goals ────────────────────────────────────────────────────────
ALTER TABLE savings_goals ADD COLUMN household_id BIGINT REFERENCES households(id);
ALTER TABLE savings_goals ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE savings_goals ADD CONSTRAINT savings_goals_owner_chk
    CHECK ((user_id IS NOT NULL) <> (household_id IS NOT NULL));
-- A household goal spans members' shared accounts and has no single owning
-- account; account_id is only meaningful for a personal goal.
ALTER TABLE savings_goals ADD CONSTRAINT savings_goals_account_personal_chk
    CHECK (account_id IS NULL OR user_id IS NOT NULL);
CREATE INDEX ix_savings_goals_household_id
    ON savings_goals (household_id)
    WHERE deleted_at IS NULL AND household_id IS NOT NULL;

-- ── drop the now-redundant household-only tables ─────────────────────────
DROP TABLE shared_budgets;
DROP TABLE shared_goals;
