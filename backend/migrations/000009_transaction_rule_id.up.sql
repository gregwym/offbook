-- #90: record which user rule (if any) categorized a transaction.
--
-- When the categorization engine matches a rule, the resulting transaction
-- row carries categorization_method='rule' (the third documented value
-- alongside 'manual' and 'plaid_default') and a pointer to the matching
-- rule's id. Nullable: most transactions won't have a rule attached
-- (manual picks, plaid_default, or uncategorized).
--
-- ON DELETE SET NULL — soft-deleting or hard-deleting a rule must not
-- cascade into history. The transaction's category_id is the source of
-- truth for what category is applied; rule_id is just provenance, and
-- losing the breadcrumb is acceptable when the rule itself is gone.
ALTER TABLE transactions
    ADD COLUMN categorization_rule_id BIGINT REFERENCES categorization_rules(id) ON DELETE SET NULL;

-- Used by the bulk re-apply endpoint (#91) to scope "rows whose method is
-- already rule-driven" cheaply. Partial index — most rows have NULL here.
CREATE INDEX ix_transactions_categorization_rule_id
    ON transactions (categorization_rule_id)
    WHERE deleted_at IS NULL AND categorization_rule_id IS NOT NULL;
