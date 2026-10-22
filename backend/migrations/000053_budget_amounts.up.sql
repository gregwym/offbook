-- Budget limit history (#580). Editing a budget's limit used to rewrite every
-- past period and the rollover carried into this one, because only the
-- current amount was stored. Each row is the limit in effect from
-- effective_from (a period start) until the next row. budgets.amount stays
-- the current limit and is still written (expand stage).
CREATE TABLE budget_amounts (
    id             BIGSERIAL PRIMARY KEY,
    budget_id      BIGINT NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
    amount         NUMERIC(30, 18) NOT NULL CHECK (amount > 0),
    effective_from DATE NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX uq_budget_amounts_budget_from ON budget_amounts (budget_id, effective_from);

-- Backfill: one row per budget at the start of its creation period, holding
-- today's amount (the only limit we know). Weeks start Monday, as in the
-- service.
INSERT INTO budget_amounts (budget_id, amount, effective_from)
SELECT id, amount,
       CASE period
           WHEN 'weekly' THEN date_trunc('week', created_at AT TIME ZONE 'UTC')::date
           WHEN 'annual' THEN date_trunc('year', created_at AT TIME ZONE 'UTC')::date
           ELSE date_trunc('month', created_at AT TIME ZONE 'UTC')::date
       END
  FROM budgets
 WHERE amount > 0;
