-- #544: user tags on transactions (TX-01). Metadata only: tags never change
-- amounts, balances, spending or budgets. Additive; '{}' = untagged.
ALTER TABLE transactions ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';
CREATE INDEX idx_transactions_tags ON transactions USING GIN (tags);
