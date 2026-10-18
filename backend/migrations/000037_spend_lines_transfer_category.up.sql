-- TX-04 / Goal #423 gate 3: a row in the system "transfer" category (Plaid
-- TRANSFER_* / LOAN_PAYMENTS_CREDIT_CARD_PAYMENT, or the user's own pick) means
-- the money only moved. When the counterpart is not tracked (a card Offbook
-- doesn't hold, an external account) there is no pair to link, so is_transfer
-- stays false and the row used to count as spending or income. Report such
-- rows as transfers through spend_lines so every aggregate that filters
-- is_transfer=FALSE agrees. The stored transactions.is_transfer is untouched.
-- Split lines are judged by their own category. Additive: new function +
-- same-shape view redefinition; no data rewritten.
CREATE OR REPLACE FUNCTION txn_in_transfer_category(cat_id BIGINT)
RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
  SELECT COALESCE(cat_id IN (SELECT id FROM categories WHERE slug = 'transfer'), FALSE)
$$;

CREATE OR REPLACE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t) OR txn_in_transfer_category(t.category_id)) AS is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount,
       (t.amount > 0 AND t.category_id IS NOT NULL AND t.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
   OR NOT txn_splits_consistent(t.id, t.amount)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t) OR txn_in_transfer_category(s.category_id)) AS is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount,
       (s.amount > 0 AND s.category_id IS NOT NULL AND s.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id
WHERE txn_splits_consistent(t.id, t.amount);
