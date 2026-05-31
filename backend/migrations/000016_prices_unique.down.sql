BEGIN;

-- Reverse #286: drop the unique constraint, restore the non-unique lookup index.
DROP INDEX IF EXISTS uq_prices_asset_quote_asof_source;

CREATE INDEX ix_prices_lookup ON prices (asset_id, quote_asset_id, as_of DESC);

COMMIT;
