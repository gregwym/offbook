-- #536: a rule can also hide the transactions it matches (TX-01). When the
-- winning rule has hide_matches, matching flow rows get hidden_at so they
-- drop out of spending, income, budgets and merchant reports. Additive;
-- false keeps the rule category-only.
ALTER TABLE categorization_rules ADD COLUMN hide_matches BOOLEAN NOT NULL DEFAULT false;
