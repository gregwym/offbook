-- #438: split transactions. One flow transaction can be allocated across
-- several categories. The parent row stays the ledger/quantity fact (amount,
-- kind, account, valuation are untouched); splits are category allocation
-- metadata only and must sum exactly to the parent amount (enforced in the
-- service, in one DB transaction).
--
-- Additive expand step: new table + new view; nothing dropped or rewritten.
CREATE TABLE transaction_splits (
    id             BIGSERIAL PRIMARY KEY,
    transaction_id BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    user_id        BIGINT NOT NULL REFERENCES users(id),
    category_id    BIGINT NULL REFERENCES categories(id),
    amount         NUMERIC(30, 18) NOT NULL,
    note           TEXT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transaction_splits_transaction ON transaction_splits (transaction_id);
CREATE INDEX idx_transaction_splits_user ON transaction_splits (user_id);

-- spend_lines: the category-allocation view every spending / budget /
-- category-series aggregate reads instead of `transactions`. An unsplit row
-- passes through as itself; a split parent is replaced by its split lines
-- (each carrying the parent's date/account/flags). Filtering on kind,
-- is_transfer, deleted_at and amount sign stays in the consuming query.
CREATE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind, t.is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind, t.is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id;
