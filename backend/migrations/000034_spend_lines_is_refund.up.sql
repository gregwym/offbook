-- #444: classify refunds in the shared spend_lines view so every spending,
-- budget, income and cash-flow aggregate agrees. A refund is a positive line
-- in a real spending category (anything but income/transfer): it nets
-- against that category's spend instead of counting as income. Uncategorized
-- positive lines stay income. Additive: raw amounts and ledger balances are
-- untouched; only a new trailing view column.
CREATE OR REPLACE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind, t.is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount,
       (t.amount > 0 AND t.category_id IS NOT NULL AND t.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind, t.is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount,
       (s.amount > 0 AND s.category_id IS NOT NULL AND s.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id;
