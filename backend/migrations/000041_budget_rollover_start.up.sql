-- #508: optional starting balance for rollover budgets (Monarch BUD-01).
-- Seeds the carryover in the budget's creation period; only read while
-- rollover is on. Pure additive expand; existing rows default to 0, which is
-- the prior behavior.
ALTER TABLE budgets
    ADD COLUMN rollover_start NUMERIC(30, 18) NOT NULL DEFAULT 0
        CONSTRAINT budgets_rollover_start_nonneg CHECK (rollover_start >= 0);
