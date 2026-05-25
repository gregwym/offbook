-- #237: Completes the position-based account model (ADR-0013).
--
-- Pre-prod: dev DBs are wiped and re-seeded; no backfill needed.
--
-- Drops the legacy two-shape representation:
--   - accounts.balance       (replaced by positions × prices)
--   - investments table      (superseded by positions + prices)
--   - transactions.currency  (derived from transactions.asset_id below)
--
-- Drops the Phase-1 scaffolding:
--   - BEFORE-INSERT triggers on accounts/users that auto-populated the
--     new FK columns; service code now sets them explicitly.
--
-- Adds:
--   - transactions.asset_id NOT NULL REFERENCES assets(id)
--
-- After this migration, positions + prices are the only fact stores for
-- account value. accounts.account_type becomes a display hint only.

-- ──────────────────────────────────────────────────────────────────────────
-- 1. Drop Phase-1 auto-populate triggers.
-- ──────────────────────────────────────────────────────────────────────────
DROP TRIGGER IF EXISTS trg_users_primary_currency_asset    ON users;
DROP TRIGGER IF EXISTS trg_accounts_primary_quote_asset    ON accounts;
DROP FUNCTION IF EXISTS set_user_primary_currency_asset();
DROP FUNCTION IF EXISTS set_account_primary_quote_asset();

-- ──────────────────────────────────────────────────────────────────────────
-- 2. Drop legacy columns and the investments table.
-- ──────────────────────────────────────────────────────────────────────────
ALTER TABLE accounts     DROP COLUMN IF EXISTS balance;
ALTER TABLE transactions DROP COLUMN IF EXISTS currency;
DROP TABLE IF EXISTS investments;

-- ──────────────────────────────────────────────────────────────────────────
-- 3. transactions.asset_id — what this transaction moves a quantity of.
-- For cash transactions the asset equals the parent account's
-- primary_quote_asset. For trades (issue #238) the cash leg and security
-- leg each carry their own asset_id and a shared transfer_pair_id.
-- ──────────────────────────────────────────────────────────────────────────
ALTER TABLE transactions ADD COLUMN asset_id BIGINT REFERENCES assets(id);
UPDATE transactions t
   SET asset_id = a.primary_quote_asset_id
  FROM accounts a
 WHERE t.account_id = a.id;
ALTER TABLE transactions ALTER COLUMN asset_id SET NOT NULL;

CREATE INDEX ix_transactions_asset_id ON transactions (asset_id);
