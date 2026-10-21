-- TX-01 revert (#565): a committed "Apply rules to existing transactions"
-- run can be undone. Each run records the prior state of every row it
-- changed, written in the same DB transaction as the row update, so undo can
-- put back exactly what the run overwrote. Append-only audit (no deleted_at),
-- like ingestion_jobs. Additive: two new tables, nothing existing changes.
CREATE TABLE rule_apply_runs (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id),
    scope         TEXT NOT NULL,
    rows_changed  INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    undone_at     TIMESTAMPTZ,
    rows_restored INTEGER,
    rows_kept     INTEGER
);

CREATE INDEX idx_rule_apply_runs_user ON rule_apply_runs (user_id, id DESC);

-- applied_updated_at is the transactions.updated_at the run wrote. Undo only
-- restores a row whose updated_at still equals it, so a later user edit or
-- sync merge is never overwritten.
CREATE TABLE rule_apply_changes (
    id                          BIGSERIAL PRIMARY KEY,
    run_id                      BIGINT NOT NULL REFERENCES rule_apply_runs(id) ON DELETE CASCADE,
    transaction_id              BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    prev_category_id            BIGINT,
    prev_categorization_method  TEXT,
    prev_categorization_rule_id BIGINT,
    prev_merchant_name          TEXT,
    prev_hidden_at              TIMESTAMPTZ,
    prev_reviewed_at            TIMESTAMPTZ,
    prev_tags                   TEXT[] NOT NULL DEFAULT '{}',
    applied_updated_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, transaction_id)
);

CREATE INDEX idx_rule_apply_changes_transaction ON rule_apply_changes (transaction_id);
