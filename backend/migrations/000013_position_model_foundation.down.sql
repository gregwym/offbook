-- Reverse #231 / ADR-0013 Phase 1.
--
-- Drop in reverse order: columns referencing assets first, then the new
-- tables. Old columns (accounts.balance, investments.market_value,
-- transactions.currency) were never touched by the up migration, so they
-- need no restoration.

DROP TRIGGER IF EXISTS trg_users_primary_currency_asset    ON users;
DROP TRIGGER IF EXISTS trg_accounts_primary_quote_asset    ON accounts;
DROP FUNCTION IF EXISTS set_user_primary_currency_asset();
DROP FUNCTION IF EXISTS set_account_primary_quote_asset();

ALTER TABLE accounts DROP COLUMN IF EXISTS primary_quote_asset_id;
ALTER TABLE users    DROP COLUMN IF EXISTS primary_currency_asset_id;

DROP TABLE IF EXISTS prices;
DROP TABLE IF EXISTS positions;
DROP TABLE IF EXISTS assets;
