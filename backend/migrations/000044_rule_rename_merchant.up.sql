-- #534: a rule can also rename the merchant (TX-01). When the winning rule
-- carries rename_merchant, matching rows get it as merchant_name so the
-- list, Top merchants and Recurring read one clean name. Additive; NULL
-- keeps the rule category-only.
ALTER TABLE categorization_rules ADD COLUMN rename_merchant TEXT;
