-- #550: a rule can also be narrowed by account and by amount (TX-01).
-- Both conditions are ANDed with the text/asset matcher. account_id NULL
-- matches every account; amount_min / amount_max bound the transaction's
-- magnitude (|amount|), inclusive, and NULL leaves that side open.
-- Additive: existing rules keep matching exactly as before.
ALTER TABLE categorization_rules
    ADD COLUMN account_id BIGINT REFERENCES accounts(id),
    ADD COLUMN amount_min NUMERIC(30, 18),
    ADD COLUMN amount_max NUMERIC(30, 18),
    ADD CONSTRAINT chk_categorization_rules_amount_range CHECK (
        (amount_min IS NULL OR amount_min >= 0)
        AND (amount_max IS NULL OR amount_max >= 0)
        AND (amount_min IS NULL OR amount_max IS NULL OR amount_min <= amount_max)
    );

CREATE INDEX idx_categorization_rules_account_id
    ON categorization_rules (account_id)
    WHERE account_id IS NOT NULL;
