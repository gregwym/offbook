BEGIN;

-- #286: dedup the prices time series and make price ingest idempotent.
--
-- prices was append-only with only a non-unique lookup index, so re-running
-- a sync could insert duplicate (asset, quote, as_of, source) rows and the
-- table grew unbounded. Add a unique constraint at the source-aware grain:
-- the same (asset, quote, as_of) may legitimately come from multiple sources
-- (e.g. a daily historical_snapshot and a future FX feed), so `source` is
-- part of the key. Within one source, a re-ingest upserts in place
-- (price_repo.Insert uses ON CONFLICT ... DO UPDATE).
--
-- The unique index's leading columns (asset_id, quote_asset_id, as_of)
-- subsume the old ix_prices_lookup, so drop it — LatestPriceAt's
-- "ORDER BY as_of DESC LIMIT 1" is served by a backward scan of the new
-- index. Net-zero storage.
--
-- Pre-prod: dev DBs are wiped and rebuilt, so no dedup of existing rows is
-- needed before the unique index is created.

DROP INDEX IF EXISTS ix_prices_lookup;

CREATE UNIQUE INDEX uq_prices_asset_quote_asof_source
    ON prices (asset_id, quote_asset_id, as_of, source);

COMMIT;
