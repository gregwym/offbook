-- #546: a rule can also add tags to the transactions it matches (TX-01).
-- When the winning rule has add_tags, matching flow rows gain any of those
-- tags they don't already carry (case-insensitive). A rule never removes a
-- tag. Additive; '{}' keeps the rule tag-free.
ALTER TABLE categorization_rules ADD COLUMN add_tags TEXT[] NOT NULL DEFAULT '{}';
