-- #231: Phase 1 of ADR-0013 — position-based account model foundation.
--
-- Adds assets / positions / prices tables, seeds common assets, adds
-- primary-currency columns on users and accounts, and backfills positions
-- and prices from existing accounts.balance + investments snapshots.
--
-- Old columns (accounts.balance, investments.market_value,
-- transactions.currency) remain populated and authoritative. No service
-- read path switches in this phase; that's Phase 2 (issue #232).
--
-- Invariant from ADR-0013: the app never invents transactions. This
-- migration inserts ZERO rows into transactions.

-- ──────────────────────────────────────────────────────────────────────────
-- 1. assets — every unit of value (fiat, equity, fund, crypto, bond, …)
-- ──────────────────────────────────────────────────────────────────────────
CREATE TABLE assets (
    id                      BIGSERIAL PRIMARY KEY,
    symbol                  TEXT NOT NULL,
    kind                    TEXT NOT NULL CHECK (kind IN
                                ('fiat','equity','fund','crypto','bond','commodity','other')),
    display_name            TEXT,
    quote_currency_asset_id BIGINT REFERENCES assets(id),
    precision               SMALLINT NOT NULL DEFAULT 8,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (symbol, kind)
);

-- Seed common fiat + crypto. Idempotent on (symbol, kind).
INSERT INTO assets (symbol, kind, display_name, precision) VALUES
    ('USD', 'fiat',   'US Dollar',         2),
    ('EUR', 'fiat',   'Euro',              2),
    ('GBP', 'fiat',   'British Pound',     2),
    ('JPY', 'fiat',   'Japanese Yen',      0),
    ('CNY', 'fiat',   'Chinese Yuan',      2),
    ('CAD', 'fiat',   'Canadian Dollar',   2),
    ('AUD', 'fiat',   'Australian Dollar', 2),
    ('CHF', 'fiat',   'Swiss Franc',       2),
    ('HKD', 'fiat',   'Hong Kong Dollar',  2),
    ('SGD', 'fiat',   'Singapore Dollar',  2),
    ('BTC', 'crypto', 'Bitcoin',           8),
    ('ETH', 'crypto', 'Ethereum',          8)
ON CONFLICT (symbol, kind) DO NOTHING;

-- ──────────────────────────────────────────────────────────────────────────
-- 2. positions — current (account × asset) holdings. Quantity is the fact.
-- ──────────────────────────────────────────────────────────────────────────
CREATE TABLE positions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id),
    account_id  BIGINT NOT NULL REFERENCES accounts(id),
    asset_id    BIGINT NOT NULL REFERENCES assets(id),
    quantity    NUMERIC(30, 18) NOT NULL,
    cost_basis  NUMERIC(30, 18),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_positions_account_asset
    ON positions (account_id, asset_id) WHERE deleted_at IS NULL;
CREATE INDEX ix_positions_user_account
    ON positions (user_id, account_id) WHERE deleted_at IS NULL;

-- ──────────────────────────────────────────────────────────────────────────
-- 3. prices — append-only (asset, quote_asset, as_of, price) time series.
-- ──────────────────────────────────────────────────────────────────────────
CREATE TABLE prices (
    id             BIGSERIAL PRIMARY KEY,
    asset_id       BIGINT NOT NULL REFERENCES assets(id),
    quote_asset_id BIGINT NOT NULL REFERENCES assets(id),
    as_of          TIMESTAMPTZ NOT NULL,
    price          NUMERIC(30, 18) NOT NULL,
    source         TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ix_prices_lookup ON prices (asset_id, quote_asset_id, as_of DESC);

-- ──────────────────────────────────────────────────────────────────────────
-- 4. users.primary_currency_asset_id — drives net worth / dashboard rollups
-- ──────────────────────────────────────────────────────────────────────────
ALTER TABLE users ADD COLUMN primary_currency_asset_id BIGINT REFERENCES assets(id);
UPDATE users
   SET primary_currency_asset_id = (SELECT id FROM assets WHERE symbol = 'USD' AND kind = 'fiat');
ALTER TABLE users ALTER COLUMN primary_currency_asset_id SET NOT NULL;

-- ──────────────────────────────────────────────────────────────────────────
-- 5. accounts.primary_quote_asset_id — the currency this account reports in
-- ──────────────────────────────────────────────────────────────────────────
ALTER TABLE accounts ADD COLUMN primary_quote_asset_id BIGINT REFERENCES assets(id);

-- Auto-seed any unseen account currencies as fiat assets so the FK backfill
-- doesn't fail on instances using currencies outside the curated seed list.
INSERT INTO assets (symbol, kind, display_name, precision)
SELECT DISTINCT a.currency, 'fiat', a.currency, 2
  FROM accounts a
 WHERE NOT EXISTS (
       SELECT 1 FROM assets ass
        WHERE ass.symbol = a.currency AND ass.kind = 'fiat'
   )
ON CONFLICT (symbol, kind) DO NOTHING;

UPDATE accounts
   SET primary_quote_asset_id = (
       SELECT id FROM assets WHERE symbol = COALESCE(accounts.currency, 'USD') AND kind = 'fiat'
   );
ALTER TABLE accounts ALTER COLUMN primary_quote_asset_id SET NOT NULL;

-- ──────────────────────────────────────────────────────────────────────────
-- 5b. BEFORE-INSERT/UPDATE triggers auto-populate the new FK columns on
-- accounts and users if the caller leaves them at NULL or 0. Phase 1 of
-- ADR-0013 deliberately does not touch service code — these triggers let
-- existing account-creation and user-signup paths keep working without
-- modification. Phase 2 will move the population into the Go layer and
-- drop these triggers.
-- ──────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION set_account_primary_quote_asset() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.primary_quote_asset_id IS NULL OR NEW.primary_quote_asset_id = 0 THEN
        -- Defensive: create the fiat asset on first encounter so the FK lookup
        -- never returns NULL for an unfamiliar currency.
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

-- ──────────────────────────────────────────────────────────────────────────
-- 6. Helper: classify free-form investments.asset_class → assets.kind.
-- Lives in pg_temp so it's session-scoped and auto-removed; we only need it
-- during this migration's data backfill.
-- ──────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION pg_temp.classify_asset_class(c TEXT) RETURNS TEXT AS $$
SELECT CASE LOWER(COALESCE(c, ''))
    WHEN 'equity'         THEN 'equity'
    WHEN 'stock'          THEN 'equity'
    WHEN 'stocks'         THEN 'equity'
    WHEN 'etf'            THEN 'fund'
    WHEN 'fund'           THEN 'fund'
    WHEN 'mutual_fund'    THEN 'fund'
    WHEN 'mutual fund'    THEN 'fund'
    WHEN 'crypto'         THEN 'crypto'
    WHEN 'cryptocurrency' THEN 'crypto'
    WHEN 'bond'           THEN 'bond'
    WHEN 'bonds'          THEN 'bond'
    WHEN 'fixed_income'   THEN 'bond'
    WHEN 'commodity'      THEN 'commodity'
    ELSE 'other'
END;
$$ LANGUAGE SQL IMMUTABLE;

-- ──────────────────────────────────────────────────────────────────────────
-- 7. Seed assets for tickers found in investments.
-- ──────────────────────────────────────────────────────────────────────────
INSERT INTO assets (symbol, kind, display_name, precision)
SELECT DISTINCT
       i.ticker,
       pg_temp.classify_asset_class(i.asset_class),
       COALESCE(i.name, i.ticker),
       8
  FROM investments i
 WHERE NOT EXISTS (
       SELECT 1 FROM assets ass
        WHERE ass.symbol = i.ticker
          AND ass.kind   = pg_temp.classify_asset_class(i.asset_class)
   )
ON CONFLICT (symbol, kind) DO NOTHING;

-- ──────────────────────────────────────────────────────────────────────────
-- 8. Backfill positions from accounts.balance.
--
-- Rule: every active account gets at least one position. For investment /
-- crypto account types where investments snapshots exist, skip this step
-- (positions come from snapshots — step 9). When no snapshots exist (e.g.
-- a Plaid brokerage with M3 sync only), fall back to a single cash-blob
-- position so the account still has value representable in the new model.
-- ──────────────────────────────────────────────────────────────────────────
INSERT INTO positions (user_id, account_id, asset_id, quantity, cost_basis, updated_at)
SELECT a.user_id,
       a.id,
       a.primary_quote_asset_id,
       a.balance,
       NULL,
       NOW()
  FROM accounts a
 WHERE a.deleted_at IS NULL
   AND (
       a.account_type NOT IN ('investment', 'crypto')
       OR NOT EXISTS (SELECT 1 FROM investments i WHERE i.account_id = a.id)
   );

-- ──────────────────────────────────────────────────────────────────────────
-- 9. Backfill positions from the latest investments snapshot per
-- (account_id, ticker). One position row per distinct holding.
-- ──────────────────────────────────────────────────────────────────────────
INSERT INTO positions (user_id, account_id, asset_id, quantity, cost_basis, updated_at)
SELECT DISTINCT ON (i.account_id, i.ticker)
       i.user_id,
       i.account_id,
       (SELECT ass.id FROM assets ass
         WHERE ass.symbol = i.ticker
           AND ass.kind   = pg_temp.classify_asset_class(i.asset_class)),
       i.quantity,
       i.cost_basis,
       NOW()
  FROM investments i
  JOIN accounts a ON a.id = i.account_id AND a.deleted_at IS NULL
 ORDER BY i.account_id, i.ticker, i.snapshot_date DESC, i.id DESC;

-- ──────────────────────────────────────────────────────────────────────────
-- 10. Backfill prices from historical investments snapshots.
-- price = market_value / quantity, quoted in the parent account's currency.
-- ──────────────────────────────────────────────────────────────────────────
INSERT INTO prices (asset_id, quote_asset_id, as_of, price, source)
SELECT (SELECT ass.id FROM assets ass
         WHERE ass.symbol = i.ticker
           AND ass.kind   = pg_temp.classify_asset_class(i.asset_class)),
       a.primary_quote_asset_id,
       i.snapshot_date::timestamptz,
       (i.market_value / i.quantity),
       'historical_snapshot'
  FROM investments i
  JOIN accounts a ON a.id = i.account_id AND a.deleted_at IS NULL
 WHERE i.market_value IS NOT NULL
   AND i.quantity > 0;
