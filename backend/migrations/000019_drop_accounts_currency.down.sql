BEGIN;

-- Restore accounts.currency, backfilled from the primary quote asset's symbol.
ALTER TABLE accounts ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD';
UPDATE accounts a
   SET currency = ass.symbol
  FROM assets ass
 WHERE ass.id = a.primary_quote_asset_id;

COMMIT;
