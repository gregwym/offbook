-- #424: independent per-transaction review state.
--
-- "Needs review" used to be inferred from categorization_method='plaid_default',
-- which conflates "how it was categorized" with "a human has looked at it". A
-- correctly categorized row can still need review, and a manually categorized
-- row may never have been confirmed. reviewed_at is explicit user state:
-- NULL = unreviewed, set = reviewed. Pure metadata (like notes/acknowledged_at)
-- — never touches ledger fields, kind, transfers, or valuation.
--
-- Additive expand step: nothing is dropped or rewritten.
ALTER TABLE transactions
    ADD COLUMN reviewed_at TIMESTAMPTZ NULL;

-- Backfill: rows that exist today are treated as already seen, except rows the
-- Plaid mapper auto-categorized and the user hasn't confirmed — those are
-- exactly what the previous "Needs review" chip surfaced, so the user's inbox
-- count is unchanged by this migration (no flood of historical rows).
UPDATE transactions
SET reviewed_at = created_at
WHERE reviewed_at IS NULL
  AND categorization_method IS DISTINCT FROM 'plaid_default';

-- The inbox query: one user's unreviewed live flow rows.
CREATE INDEX idx_transactions_unreviewed
    ON transactions (user_id, transaction_date DESC)
    WHERE reviewed_at IS NULL AND deleted_at IS NULL;
