-- #445: a split parent's amount can legitimately change underneath its splits
-- (Plaid pending -> posted revises the amount). The ledger amount always
-- follows the bank; the user's split rows are preserved untouched, but while
-- they no longer sum to the parent they are STALE: spend_lines falls back to
-- the parent row (posted amount, parent category) so category / budget /
-- month / household totals stay correct instead of reporting the old
-- allocation as fully allocated. Re-saving the split (or an amount that
-- matches again) restores the allocation. Derived, no stored flag.
-- Additive: new function + same-shape view redefinition; no data rewritten.
CREATE OR REPLACE FUNCTION txn_splits_consistent(txn_id BIGINT, txn_amount NUMERIC)
RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
  SELECT COALESCE((SELECT SUM(s.amount) FROM transaction_splits s WHERE s.transaction_id = txn_id), 0) = txn_amount
$$;

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
