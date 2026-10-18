CREATE OR REPLACE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t)) AS is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount,
       (t.amount > 0 AND t.category_id IS NOT NULL AND t.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
   OR NOT txn_splits_consistent(t.id, t.amount)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t)) AS is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount,
       (s.amount > 0 AND s.category_id IS NOT NULL AND s.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id
WHERE txn_splits_consistent(t.id, t.amount);

DROP FUNCTION IF EXISTS txn_in_transfer_category(BIGINT);
