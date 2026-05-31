BEGIN;

-- #284: accounts.currency duplicated primary_quote_asset_id. For a fiat asset
-- assets.symbol IS the currency code, so accounts.currency was always equal to
-- (SELECT symbol FROM assets WHERE id = accounts.primary_quote_asset_id). The
-- asset FK is the single source of truth; the string column is derived.
--
-- Currency is now resolved from the asset on read (service hydration + the
-- household aggregator joins assets). API/UI still see a `currency` field.
ALTER TABLE accounts DROP COLUMN IF EXISTS currency;

COMMIT;
