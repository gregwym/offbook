-- Reverse #237. Restores legacy columns / tables / triggers so the migration
-- round-trip test (TestMigrations_UpDownUpIsIdempotent) sees the same schema
-- after down→up as before. Pre-prod, so no data preservation concern.

-- Drop the asset_id FK on transactions.
DROP INDEX IF EXISTS ix_transactions_asset_id;
ALTER TABLE transactions DROP COLUMN IF EXISTS asset_id;

-- Restore investments table (mirrors 000001_init.up.sql).
CREATE TABLE investments (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    ticker          TEXT NOT NULL,
    name            TEXT,
    asset_class     TEXT,
    quantity        NUMERIC(30, 18) NOT NULL,
    cost_basis      NUMERIC(30, 18),
    market_value    NUMERIC(30, 18),
    snapshot_date   DATE NOT NULL,
    source          TEXT NOT NULL CHECK (source IN ('plaid','csv','manual')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_investments_user_id      ON investments (user_id);
CREATE INDEX ix_investments_account_snapshot
    ON investments (account_id, snapshot_date DESC);
CREATE INDEX ix_investments_ticker
    ON investments (ticker, snapshot_date DESC);

-- Restore transactions.currency.
ALTER TABLE transactions ADD COLUMN currency TEXT NOT NULL DEFAULT 'USD';

-- Restore accounts.balance.
ALTER TABLE accounts ADD COLUMN balance NUMERIC(30, 18) NOT NULL DEFAULT 0;

-- Restore Phase-1 triggers (mirror 000013_position_model_foundation.up.sql §5b).
CREATE OR REPLACE FUNCTION set_account_primary_quote_asset() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.primary_quote_asset_id IS NULL OR NEW.primary_quote_asset_id = 0 THEN
        INSERT INTO assets (symbol, kind, display_name, precision)
        VALUES (COALESCE(NEW.currency, 'USD'), 'fiat', COALESCE(NEW.currency, 'USD'), 2)
        ON CONFLICT (symbol, kind) DO NOTHING;
        NEW.primary_quote_asset_id := (
            SELECT id FROM assets WHERE symbol = COALESCE(NEW.currency, 'USD') AND kind = 'fiat'
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_accounts_primary_quote_asset
    BEFORE INSERT OR UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION set_account_primary_quote_asset();

CREATE OR REPLACE FUNCTION set_user_primary_currency_asset() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.primary_currency_asset_id IS NULL OR NEW.primary_currency_asset_id = 0 THEN
        NEW.primary_currency_asset_id := (SELECT id FROM assets WHERE symbol = 'USD' AND kind = 'fiat');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_primary_currency_asset
    BEFORE INSERT OR UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_user_primary_currency_asset();
