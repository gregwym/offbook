-- #447: money moving between the user's own accounts must not inflate income
-- or spending while its match is still an unconfirmed proposal (#371). A
-- live, unlinked flow row is a "possible transfer" when another live,
-- unlinked flow row of the same user sits in a different account, in the
-- same asset, with the exact opposite amount, within 3 days (mirrors
-- ListTransferCandidates / defaultTransferMatchWindowDays). Such rows report
-- is_transfer=TRUE through spend_lines so every aggregate that already
-- filters is_transfer=FALSE agrees. The stored transactions.is_transfer is
-- untouched: confirming or ignoring the proposal stays a user decision, and
-- ignoring it (unlink) is the only way these rows count again.
-- Additive: new function + same-shape view redefinition; no data rewritten.
CREATE OR REPLACE FUNCTION txn_is_proposed_transfer(t transactions)
RETURNS BOOLEAN
LANGUAGE sql STABLE AS $$
  SELECT t.kind = 'flow'
     AND NOT t.is_transfer
     AND t.transfer_pair_id IS NULL
     AND EXISTS (
       SELECT 1 FROM transactions o
       WHERE o.user_id = t.user_id
         AND o.deleted_at IS NULL
         AND o.kind = 'flow'
         AND NOT o.is_transfer
         AND o.transfer_pair_id IS NULL
         AND o.id <> t.id
         AND o.account_id <> t.account_id
         AND o.asset_id = t.asset_id
         AND o.amount = -t.amount
         AND ABS(o.transaction_date - t.transaction_date) <= 3
     )
$$;

CREATE OR REPLACE VIEW spend_lines AS
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t)) AS is_transfer,
       t.transaction_date, t.deleted_at, t.category_id, t.amount,
       (t.amount > 0 AND t.category_id IS NOT NULL AND t.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
WHERE NOT EXISTS (SELECT 1 FROM transaction_splits s WHERE s.transaction_id = t.id)
UNION ALL
SELECT t.id, t.user_id, t.account_id, t.kind,
       (t.is_transfer OR txn_is_proposed_transfer(t)) AS is_transfer,
       t.transaction_date, t.deleted_at, s.category_id, s.amount,
       (s.amount > 0 AND s.category_id IS NOT NULL AND s.category_id NOT IN
          (SELECT id FROM categories WHERE slug IN ('income', 'transfer'))) AS is_refund
FROM transactions t
JOIN transaction_splits s ON s.transaction_id = t.id;
