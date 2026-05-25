-- #238 (M10b): let categorization rules target a specific asset.
--
-- Trade ingestion (#238) writes paired transaction rows where the security
-- leg carries the security's asset_id (AAPL, BTC, …). A rule like
-- "all AAPL buys → Investments category" needs to match on asset_id, not
-- only on description/merchant text.
--
-- asset_id is nullable: NULL keeps the rule asset-agnostic (existing
-- behavior). When set, the rule only fires on transactions whose
-- asset_id matches — combined with the text matcher if a pattern is
-- supplied.
ALTER TABLE categorization_rules
    ADD COLUMN asset_id BIGINT REFERENCES assets(id);

CREATE INDEX ix_categorization_rules_asset_id
    ON categorization_rules (asset_id)
    WHERE deleted_at IS NULL AND asset_id IS NOT NULL;
