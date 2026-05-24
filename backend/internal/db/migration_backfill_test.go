package db_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestMigration000013_PositionModelBackfill verifies the ADR-0013 Phase 1
// migration (#231):
//   - new tables (assets, positions, prices) are created and the seed
//     fiat/crypto assets are present
//   - users.primary_currency_asset_id and accounts.primary_quote_asset_id
//     are populated for every pre-existing row
//   - positions are backfilled correctly:
//   - cash-type accounts produce one position (asset = currency, qty = balance)
//   - investment accounts with snapshots produce one position per ticker
//     using the LATEST snapshot's quantity + cost_basis
//   - investment accounts without snapshots fall back to a single cash-blob
//   - prices are backfilled from every historical investments snapshot
//   - transactions table is untouched (the app never invents transactions)
//   - the auto-populate triggers on accounts/users fire correctly when a
//     new row is inserted post-migration without the FK columns set
func TestMigration000013_PositionModelBackfill(t *testing.T) {
	loadRepoDotenv(t)
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("DATABASE_URL")
	}
	if baseURL == "" {
		t.Skip("no DATABASE_URL set; skipping migration backfill test")
	}

	migrationsPath, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}

	ephemeral, cleanup := createEphemeralDB(t, baseURL)
	defer cleanup()

	m, closeFn := newMigrator(t, ephemeral, migrationsPath)
	defer closeFn()

	// 1. Bring schema up to N-1 (= 12). The position-model migration is 13.
	mustMigrateTo(t, m, 12)

	conn, err := sql.Open("postgres", ephemeral)
	if err != nil {
		t.Fatalf("open ephemeral: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 2. Seed fixture rows. Mirrors a typical post-M6 state: one user with
	//    a cash account, an investment account holding two snapshots of one
	//    ticker, and a Plaid-style investment account with NO snapshots (so
	//    the fallback cash-blob backfill kicks in).
	var userID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, last_scope, default_scope)
		VALUES ('m13-test@example.com', 'x', 'personal', 'personal')
		RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	var checkingID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO accounts (user_id, name, institution_slug, account_type, currency, balance)
		VALUES ($1, 'fixture-checking', 'fix', 'checking', 'USD', 1000.00)
		RETURNING id`, userID).Scan(&checkingID); err != nil {
		t.Fatalf("seed checking: %v", err)
	}

	var brokerageWithHoldingsID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO accounts (user_id, name, institution_slug, account_type, currency, balance)
		VALUES ($1, 'fixture-brokerage', 'fix', 'investment', 'USD', 1820.00)
		RETURNING id`, userID).Scan(&brokerageWithHoldingsID); err != nil {
		t.Fatalf("seed brokerage: %v", err)
	}

	var brokerageNoHoldingsID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO accounts (user_id, name, institution_slug, account_type, currency, balance)
		VALUES ($1, 'fixture-plaid-only', 'fix', 'investment', 'USD', 50000.00)
		RETURNING id`, userID).Scan(&brokerageNoHoldingsID); err != nil {
		t.Fatalf("seed plaid-only brokerage: %v", err)
	}

	// Two AAPL snapshots: older at $170, newer at $182. Latest qty=10.
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO investments
		  (user_id, account_id, ticker, name, asset_class, quantity, cost_basis, market_value, snapshot_date, source)
		VALUES
		  ($1, $2, 'AAPL', 'Apple Inc.', 'equity', 10,   1500, 1700, '2025-01-15', 'csv'),
		  ($1, $2, 'AAPL', 'Apple Inc.', 'equity', 10,   1500, 1820, '2025-05-15', 'csv')`,
		userID, brokerageWithHoldingsID); err != nil {
		t.Fatalf("seed investments: %v", err)
	}

	txCountBefore := scalarInt(t, conn, "SELECT COUNT(*) FROM transactions")

	// 3. Apply migration 13.
	mustMigrateTo(t, m, 13)

	// 4. Assertions.

	// 4a. Seed assets are present.
	for _, sym := range []string{"USD", "EUR", "BTC", "ETH"} {
		var id int64
		if err := conn.QueryRowContext(ctx,
			`SELECT id FROM assets WHERE symbol = $1 AND kind IN ('fiat','crypto') LIMIT 1`, sym).Scan(&id); err != nil {
			t.Errorf("seed asset %s not present: %v", sym, err)
		}
	}

	// AAPL was added via the investments backfill, classified as equity.
	var aaplID int64
	if err := conn.QueryRowContext(ctx,
		`SELECT id FROM assets WHERE symbol = 'AAPL' AND kind = 'equity'`).Scan(&aaplID); err != nil {
		t.Fatalf("AAPL asset not created: %v", err)
	}

	// 4b. users.primary_currency_asset_id populated for the pre-existing user.
	var pcid sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT primary_currency_asset_id FROM users WHERE id = $1`, userID).Scan(&pcid); err != nil {
		t.Fatalf("read user primary_currency_asset_id: %v", err)
	}
	if !pcid.Valid {
		t.Errorf("user primary_currency_asset_id is NULL after migration")
	}

	// 4c. accounts.primary_quote_asset_id populated for every account.
	for _, id := range []int64{checkingID, brokerageWithHoldingsID, brokerageNoHoldingsID} {
		var pqaid sql.NullInt64
		if err := conn.QueryRowContext(ctx,
			`SELECT primary_quote_asset_id FROM accounts WHERE id = $1`, id).Scan(&pqaid); err != nil {
			t.Fatalf("read account %d primary_quote_asset_id: %v", id, err)
		}
		if !pqaid.Valid {
			t.Errorf("account %d primary_quote_asset_id is NULL after migration", id)
		}
	}

	// 4d. positions: checking has 1 USD position with qty 1000.
	checkingPositions := listPositions(t, conn, ctx, checkingID)
	if len(checkingPositions) != 1 {
		t.Errorf("checking: want 1 position, got %d", len(checkingPositions))
	} else if checkingPositions[0].quantity != "1000.000000000000000000" {
		t.Errorf("checking position qty = %q, want 1000.000000000000000000", checkingPositions[0].quantity)
	}

	// 4e. brokerage with holdings: 1 AAPL position with qty 10 (from the
	//     LATEST snapshot — not summed across snapshots).
	brokeragePositions := listPositions(t, conn, ctx, brokerageWithHoldingsID)
	if len(brokeragePositions) != 1 {
		t.Errorf("brokerage-w-holdings: want 1 position, got %d", len(brokeragePositions))
	} else {
		if brokeragePositions[0].assetID != aaplID {
			t.Errorf("brokerage position asset_id = %d, want %d (AAPL)", brokeragePositions[0].assetID, aaplID)
		}
		if brokeragePositions[0].quantity != "10.000000000000000000" {
			t.Errorf("brokerage position qty = %q, want 10", brokeragePositions[0].quantity)
		}
	}

	// 4f. brokerage WITHOUT holdings: falls back to a cash-blob USD position
	//     so the account still has value representable in the new model.
	fallbackPositions := listPositions(t, conn, ctx, brokerageNoHoldingsID)
	if len(fallbackPositions) != 1 {
		t.Errorf("brokerage-no-holdings: want 1 fallback position, got %d", len(fallbackPositions))
	} else if fallbackPositions[0].quantity != "50000.000000000000000000" {
		t.Errorf("fallback position qty = %q, want 50000", fallbackPositions[0].quantity)
	}

	// 4g. prices: 2 rows for AAPL (one per snapshot).
	var aaplPriceCount int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM prices WHERE asset_id = $1`, aaplID).Scan(&aaplPriceCount); err != nil {
		t.Fatalf("count AAPL prices: %v", err)
	}
	if aaplPriceCount != 2 {
		t.Errorf("AAPL price rows = %d, want 2 (one per snapshot)", aaplPriceCount)
	}

	// 4h. transactions table untouched.
	txCountAfter := scalarInt(t, conn, "SELECT COUNT(*) FROM transactions")
	if txCountAfter != txCountBefore {
		t.Errorf("transactions row count changed: before=%d after=%d (the app must not invent transactions)",
			txCountBefore, txCountAfter)
	}

	// 4i. Triggers: insert a new account WITHOUT primary_quote_asset_id and
	//     verify the trigger populated it. Same for users.
	var triggerAcctID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO accounts (user_id, name, institution_slug, account_type, currency, balance)
		VALUES ($1, 'trigger-check', 'fix', 'checking', 'EUR', 0)
		RETURNING id`, userID).Scan(&triggerAcctID); err != nil {
		t.Fatalf("insert trigger-check account: %v", err)
	}
	var triggerAcctPQAID sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT primary_quote_asset_id FROM accounts WHERE id = $1`, triggerAcctID).Scan(&triggerAcctPQAID); err != nil {
		t.Fatalf("read trigger-check FK: %v", err)
	}
	if !triggerAcctPQAID.Valid {
		t.Errorf("account trigger did not populate primary_quote_asset_id")
	}

	var triggerUserID int64
	if err := conn.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, last_scope, default_scope)
		VALUES ('m13-trigger@example.com', 'x', 'personal', 'personal')
		RETURNING id`).Scan(&triggerUserID); err != nil {
		t.Fatalf("insert trigger user: %v", err)
	}
	var triggerUserPCID sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT primary_currency_asset_id FROM users WHERE id = $1`, triggerUserID).Scan(&triggerUserPCID); err != nil {
		t.Fatalf("read trigger user FK: %v", err)
	}
	if !triggerUserPCID.Valid {
		t.Errorf("user trigger did not populate primary_currency_asset_id")
	}
}

type positionRow struct {
	assetID  int64
	quantity string
}

func listPositions(t *testing.T, conn *sql.DB, ctx context.Context, accountID int64) []positionRow {
	t.Helper()
	rows, err := conn.QueryContext(ctx,
		`SELECT asset_id, quantity::text FROM positions WHERE account_id = $1 AND deleted_at IS NULL ORDER BY id`,
		accountID)
	if err != nil {
		t.Fatalf("list positions for account %d: %v", accountID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []positionRow
	for rows.Next() {
		var p positionRow
		if err := rows.Scan(&p.assetID, &p.quantity); err != nil {
			t.Fatalf("scan position: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("position rows.Err: %v", err)
	}
	return out
}

func scalarInt(t *testing.T, conn *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("scalarInt %q: %v", q, err)
	}
	return n
}
