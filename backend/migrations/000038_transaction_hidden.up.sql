-- TX-02 / Goal #423 (#465): a user can hide a flow transaction from spending,
-- income, budgets and category reports without deleting it. The row stays in
-- the ledger (balances, net worth, positions are untouched); only the report
-- surfaces drop it. spend_lines reports hidden rows (and each split line of
-- them) through its is_transfer flag, so every aggregate that already filters
-- is_transfer=FALSE agrees. Additive: nullable column + same-shape view.
ALTER TABLE transactions ADD COLUMN hidden_at TIMESTAMPTZ;

CREATE OR REPLACE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t) OR txn_in_transfer_category(t.category_id) OR t.hidden_at IS NOT NULL) AS is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount,
       (t.amount > 0 AND t.category_id IS NOT NULL AND t.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
   OR NOT txn_splits_consistent(t.id, t.amount)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t) OR txn_in_transfer_category(s.category_id) OR t.hidden_at IS NOT NULL) AS is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount,
       (s.amount > 0 AND s.category_id IS NOT NULL AND s.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id
WHERE txn_splits_consistent(t.id, t.amount);
