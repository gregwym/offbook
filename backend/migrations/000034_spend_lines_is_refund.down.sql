DROP VIEW spend_lines;
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
