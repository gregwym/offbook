-- #64: persistent mapping from Plaid personal_finance_category (PFC) to
-- our internal categories. Lives in SQL (not Go literals) so an admin can
-- edit/extend the table without a Go release and so the mapping is
-- reviewable in git diffs.
--
-- PK is (plaid_primary, plaid_detailed). A row with NO mapping = the
-- transaction lands uncategorized and the user picks. Manual choices
-- always win on update — see transaction_mapping.go.
CREATE TABLE plaid_category_map (
    plaid_primary  TEXT   NOT NULL,
    plaid_detailed TEXT   NOT NULL,
    category_id    BIGINT NOT NULL REFERENCES categories(id),
    PRIMARY KEY (plaid_primary, plaid_detailed)
);
CREATE INDEX ix_plaid_category_map_category_id ON plaid_category_map (category_id);

-- Seed common Plaid PFCs. Reference taxonomy:
--   https://plaid.com/documents/transactions-personal-finance-category-taxonomy.csv
-- Insert by joining against categories.slug so the mapping survives a
-- categories.id reshuffle (system categories are seeded in 000002).
INSERT INTO plaid_category_map (plaid_primary, plaid_detailed, category_id)
SELECT v.plaid_primary, v.plaid_detailed, c.id
FROM (VALUES
    -- INCOME → income
    ('INCOME', 'WAGES',                              'income'),
    ('INCOME', 'TAX_REFUND',                         'income'),
    ('INCOME', 'DIVIDENDS',                          'income'),
    ('INCOME', 'INTEREST_EARNED',                    'income'),
    ('INCOME', 'RETIREMENT_PENSION',                 'income'),
    ('INCOME', 'UNEMPLOYMENT',                       'income'),
    ('INCOME', 'OTHER_INCOME',                       'income'),

    -- TRANSFER_IN / TRANSFER_OUT → transfer
    -- (We do map these to the system "Transfer" category. Down-stream
    --  spending math should filter on is_transfer / category=transfer
    --  rather than rely on an unmapped null.)
    ('TRANSFER_IN',  'ACCOUNT_TRANSFER',             'transfer'),
    ('TRANSFER_IN',  'DEPOSIT',                      'transfer'),
    ('TRANSFER_IN',  'SAVINGS',                      'transfer'),
    ('TRANSFER_IN',  'OTHER_TRANSFER_IN',            'transfer'),
    ('TRANSFER_OUT', 'ACCOUNT_TRANSFER',             'transfer'),
    ('TRANSFER_OUT', 'WITHDRAWAL',                   'transfer'),
    ('TRANSFER_OUT', 'SAVINGS',                      'transfer'),
    ('TRANSFER_OUT', 'OTHER_TRANSFER_OUT',           'transfer'),

    -- LOAN_PAYMENTS — mostly bills, mortgage is its own bucket
    ('LOAN_PAYMENTS', 'MORTGAGE_PAYMENT',            'rent-mortgage'),
    ('LOAN_PAYMENTS', 'CAR_PAYMENT',                 'transportation'),
    ('LOAN_PAYMENTS', 'STUDENT_LOAN_PAYMENT',        'education'),
    -- credit_card_payment intentionally unmapped: it's an inter-account
    -- transfer, not spending. User chooses if they want a category.

    -- BANK_FEES → fees-and-charges
    ('BANK_FEES', 'ATM_FEES',                        'fees-and-charges'),
    ('BANK_FEES', 'FOREIGN_TRANSACTION_FEES',        'fees-and-charges'),
    ('BANK_FEES', 'INSUFFICIENT_FUNDS',              'fees-and-charges'),
    ('BANK_FEES', 'INTEREST_CHARGE',                 'fees-and-charges'),
    ('BANK_FEES', 'OVERDRAFT_FEES',                  'fees-and-charges'),
    ('BANK_FEES', 'OTHER_BANK_FEES',                 'fees-and-charges'),

    -- ENTERTAINMENT → entertainment
    ('ENTERTAINMENT', 'TV_AND_MOVIES',               'entertainment'),
    ('ENTERTAINMENT', 'MUSIC_AND_AUDIO',             'entertainment'),
    ('ENTERTAINMENT', 'VIDEO_GAMES',                 'entertainment'),
    ('ENTERTAINMENT', 'SPORTING_EVENTS_AMUSEMENT_PARKS_AND_MUSEUMS', 'entertainment'),
    ('ENTERTAINMENT', 'CASINOS_AND_GAMBLING',        'entertainment'),
    ('ENTERTAINMENT', 'OTHER_ENTERTAINMENT',         'entertainment'),

    -- FOOD_AND_DRINK
    ('FOOD_AND_DRINK', 'GROCERIES',                  'groceries'),
    ('FOOD_AND_DRINK', 'RESTAURANT',                 'food-and-dining'),
    ('FOOD_AND_DRINK', 'FAST_FOOD',                  'food-and-dining'),
    ('FOOD_AND_DRINK', 'COFFEE',                     'food-and-dining'),
    ('FOOD_AND_DRINK', 'BEER_WINE_AND_LIQUOR',       'food-and-dining'),
    ('FOOD_AND_DRINK', 'VENDING_MACHINES',           'food-and-dining'),
    ('FOOD_AND_DRINK', 'OTHER_FOOD_AND_DRINK',       'food-and-dining'),

    -- GENERAL_MERCHANDISE → shopping (with clothing carve-out)
    ('GENERAL_MERCHANDISE', 'CLOTHING_AND_ACCESSORIES', 'clothing'),
    ('GENERAL_MERCHANDISE', 'DEPARTMENT_STORES',     'shopping'),
    ('GENERAL_MERCHANDISE', 'ONLINE_MARKETPLACES',   'shopping'),
    ('GENERAL_MERCHANDISE', 'SUPERSTORES',           'shopping'),
    ('GENERAL_MERCHANDISE', 'OTHER_GENERAL_MERCHANDISE', 'shopping'),

    -- HOME_IMPROVEMENT → housing
    ('HOME_IMPROVEMENT', 'FURNITURE',                'housing'),
    ('HOME_IMPROVEMENT', 'HARDWARE',                 'housing'),
    ('HOME_IMPROVEMENT', 'REPAIR_AND_MAINTENANCE',   'housing'),
    ('HOME_IMPROVEMENT', 'OTHER_HOME_IMPROVEMENT',   'housing'),

    -- MEDICAL → healthcare
    ('MEDICAL', 'DENTAL_CARE',                       'healthcare'),
    ('MEDICAL', 'EYE_CARE',                          'healthcare'),
    ('MEDICAL', 'PHARMACIES_AND_SUPPLEMENTS',        'healthcare'),
    ('MEDICAL', 'PRIMARY_CARE',                      'healthcare'),
    ('MEDICAL', 'VETERINARY_SERVICES',               'healthcare'),
    ('MEDICAL', 'OTHER_MEDICAL',                     'healthcare'),

    -- PERSONAL_CARE → personal-care
    ('PERSONAL_CARE', 'HAIR_AND_BEAUTY',             'personal-care'),
    ('PERSONAL_CARE', 'LAUNDRY_AND_DRY_CLEANING',    'personal-care'),
    ('PERSONAL_CARE', 'GYMS_AND_FITNESS_CENTERS',    'personal-care'),
    ('PERSONAL_CARE', 'OTHER_PERSONAL_CARE',         'personal-care'),

    -- GENERAL_SERVICES — split between insurance and shopping; education has its own
    ('GENERAL_SERVICES', 'INSURANCE',                'insurance'),
    ('GENERAL_SERVICES', 'EDUCATION',                'education'),
    ('GENERAL_SERVICES', 'SUBSCRIPTIONS',            'subscriptions'),

    -- GOVERNMENT_AND_NON_PROFIT
    ('GOVERNMENT_AND_NON_PROFIT', 'DONATIONS',       'gifts-and-donations'),

    -- TRANSPORTATION
    ('TRANSPORTATION', 'GAS',                        'gas'),
    ('TRANSPORTATION', 'PUBLIC_TRANSIT',             'transportation'),
    ('TRANSPORTATION', 'TAXIS_AND_RIDE_SHARES',      'transportation'),
    ('TRANSPORTATION', 'PARKING',                    'transportation'),
    ('TRANSPORTATION', 'TOLLS',                      'transportation'),
    ('TRANSPORTATION', 'BIKES_AND_SCOOTERS',         'transportation'),
    ('TRANSPORTATION', 'OTHER_TRANSPORTATION',       'transportation'),

    -- TRAVEL
    ('TRAVEL', 'FLIGHTS',                            'travel'),
    ('TRAVEL', 'LODGING',                            'travel'),
    ('TRAVEL', 'RENTAL_CARS',                        'travel'),
    ('TRAVEL', 'OTHER_TRAVEL',                       'travel'),

    -- RENT_AND_UTILITIES
    ('RENT_AND_UTILITIES', 'RENT',                       'rent-mortgage'),
    ('RENT_AND_UTILITIES', 'GAS_AND_ELECTRICITY',        'utilities'),
    ('RENT_AND_UTILITIES', 'INTERNET_AND_CABLE',         'utilities'),
    ('RENT_AND_UTILITIES', 'TELEPHONE',                  'utilities'),
    ('RENT_AND_UTILITIES', 'WATER',                      'utilities'),
    ('RENT_AND_UTILITIES', 'SEWAGE_AND_WASTE_MANAGEMENT','utilities'),
    ('RENT_AND_UTILITIES', 'OTHER_UTILITIES',            'utilities')
) AS v(plaid_primary, plaid_detailed, slug)
JOIN categories c ON c.slug = v.slug AND c.deleted_at IS NULL
ON CONFLICT (plaid_primary, plaid_detailed) DO NOTHING;
