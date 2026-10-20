-- #510: the user can say a proposed transfer pair (#371) is "not a transfer".
-- A dismissal is stored per unordered pair (lower id first) so the matcher
-- never proposes it again, across resyncs that keep the row ids, and so
-- spend_lines stops excluding the two rows from income, spending and
-- budgets. Pure additive expand: new table + a same-signature redefinition
-- of txn_is_proposed_transfer (the view picks it up without a rebuild).
CREATE TABLE transfer_match_dismissals (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id),
    transaction_low_id  BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    transaction_high_id BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,
    CONSTRAINT transfer_match_dismissals_ordered CHECK (transaction_low_id < transaction_high_id)
);

CREATE UNIQUE INDEX uq_transfer_match_dismissals_pair
    ON transfer_match_dismissals (user_id, transaction_low_id, transaction_high_id)
    WHERE deleted_at IS NULL;
CREATE INDEX ix_transfer_match_dismissals_high
    ON transfer_match_dismissals (transaction_high_id)
    WHERE deleted_at IS NULL;

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
         AND NOT EXISTS (
           SELECT 1 FROM transfer_match_dismissals d
           WHERE d.user_id = t.user_id
             AND d.deleted_at IS NULL
             AND d.transaction_low_id = LEAST(o.id, t.id)
             AND d.transaction_high_id = GREATEST(o.id, t.id)
         )
     )
$$;
