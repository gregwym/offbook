-- #195: fresh Plaid sandbox sync left 9/42 transactions uncategorized (78.6%,
-- below the #181 acceptance bar of >=80%). All 9 traced to three valid Plaid
-- PFC pairs the 000005 seed never covered. Rows are already in the
-- 000012-normalized prefixed form (primary repeated in detailed).
INSERT INTO plaid_category_map (plaid_primary, plaid_detailed, category_id)
SELECT v.plaid_primary, v.plaid_detailed, c.id
FROM (VALUES
    -- Gift/novelty purchases are ordinary shopping.
    ('GENERAL_MERCHANDISE', 'GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES', 'shopping'),
    -- Credit-card and generic loan "other" payments are inter-account
    -- transfers, not spending, same rationale as the intentionally-unmapped
    -- credit_card_payment note in 000005.
    ('LOAN_PAYMENTS', 'LOAN_PAYMENTS_CREDIT_CARD_PAYMENT', 'transfer'),
    ('LOAN_PAYMENTS', 'LOAN_PAYMENTS_OTHER_PAYMENT', 'transfer')
) AS v(plaid_primary, plaid_detailed, slug)
JOIN categories c ON c.slug = v.slug AND c.deleted_at IS NULL
ON CONFLICT (plaid_primary, plaid_detailed) DO NOTHING;
