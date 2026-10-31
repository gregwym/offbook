-- Goal contribution history (#682). A contribution used to be a bare
-- UPDATE of savings_goals.current_amount, so nothing recorded when or how
-- much was added. Each row is one contribution (amount > 0) or withdrawal
-- (amount < 0) and the goal's total right after it, written in the same
-- transaction as the UPDATE. balance_after keeps the running total exact
-- even for goals that held money before history was kept.
--
-- Append-only audit log: no deleted_at. user_id is who recorded it (needed
-- for a household goal; personal attribution is implicit). It is SET NULL
-- if that user is hard-deleted so the goal's history survives.
CREATE TABLE savings_goal_contributions (
    id            BIGSERIAL PRIMARY KEY,
    goal_id       BIGINT NOT NULL REFERENCES savings_goals(id) ON DELETE CASCADE,
    user_id       BIGINT REFERENCES users(id) ON DELETE SET NULL,
    amount        NUMERIC(30, 18) NOT NULL CHECK (amount <> 0),
    balance_after NUMERIC(30, 18) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_savings_goal_contributions_goal ON savings_goal_contributions (goal_id, id);
CREATE INDEX ix_savings_goal_contributions_user_id ON savings_goal_contributions (user_id) WHERE user_id IS NOT NULL;
