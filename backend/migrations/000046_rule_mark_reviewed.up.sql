-- #538: a rule can also mark the transactions it matches reviewed (TX-01).
-- When the winning rule has mark_reviewed, matching flow rows get
-- reviewed_at so trusted recurring merchants skip the Needs review inbox.
-- Additive; false keeps the rule category-only.
ALTER TABLE categorization_rules ADD COLUMN mark_reviewed BOOLEAN NOT NULL DEFAULT false;
