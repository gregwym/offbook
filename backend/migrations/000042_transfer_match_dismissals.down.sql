-- Restore the 000035 definition before dropping the table it references.
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

DROP TABLE IF EXISTS transfer_match_dismissals;
