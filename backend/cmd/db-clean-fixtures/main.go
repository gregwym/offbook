// One-shot cleanup of integration-test fixture rows from a DB that was
// polluted before env isolation (#183) split dev and test into separate
// databases.
//
// Targets:
//   - users with `@example.test` emails (the standard test-fixture marker)
//     and every owned row that hangs off them
//   - non-system categories whose names match the timestamp-suffix pattern
//     `XX-NNNNNN.NNNNNN` produced by the integration suite (AlertCat-*,
//     IsoAlert-*, EdgeCat-*, CtxGroc-*, Inactive-*, …)
//
// Usage (against the dev DB):
//
//	cd backend && go run ./cmd/db-clean-fixtures            # dry run
//	cd backend && go run ./cmd/db-clean-fixtures --apply    # actually delete
//
// Safe to run repeatedly. Refuses to run against a URL whose database name
// is `offbook_test` — that DB is supposed to be full of fixtures.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"

	_ "github.com/lib/pq"

	"github.com/gregwym/offbook/backend/internal/config"
)

func main() {
	apply := flag.Bool("apply", false, "actually delete (default is dry-run)")
	flag.Parse()

	cfg := config.MustLoad()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is empty after config.Load — refusing to run")
	}
	dbName, err := databaseName(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("parse DATABASE_URL: %v", err)
	}
	if dbName == "offbook_test" {
		log.Fatal("refusing to run against offbook_test — that DB is meant to hold fixtures")
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}
	ctx := context.Background()

	fmt.Printf("connected to %s (apply=%v)\n\n", dbName, *apply)

	// Each step prints its row count regardless of mode. In dry-run we use
	// SELECT-style counts; in apply mode we use DELETE … RETURNING-style
	// counts via RowsAffected so the numbers report what really happened.
	steps := []cleanupStep{
		// Fixture user emails follow `*-<unix-nanos>-…@example.test` and
		// adjacent patterns. The @example.test suffix is the reliable marker.
		{
			label: "household_invites for fixture users",
			countSQL: `SELECT COUNT(*) FROM household_invites WHERE created_by IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM household_invites WHERE created_by IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "household_members for fixture users",
			countSQL: `SELECT COUNT(*) FROM household_members WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM household_members WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "account_shares for fixture-owned accounts",
			countSQL: `SELECT COUNT(*) FROM account_shares WHERE account_id IN
				(SELECT id FROM accounts WHERE user_id IN
					(SELECT id FROM users WHERE email LIKE '%@example.test'))`,
			deleteSQL: `DELETE FROM account_shares WHERE account_id IN
				(SELECT id FROM accounts WHERE user_id IN
					(SELECT id FROM users WHERE email LIKE '%@example.test'))`,
		},
		{
			label: "ai_messages for fixture users (via thread)",
			countSQL: `SELECT COUNT(*) FROM ai_messages WHERE thread_id IN
				(SELECT id FROM ai_threads WHERE user_id IN
					(SELECT id FROM users WHERE email LIKE '%@example.test'))`,
			deleteSQL: `DELETE FROM ai_messages WHERE thread_id IN
				(SELECT id FROM ai_threads WHERE user_id IN
					(SELECT id FROM users WHERE email LIKE '%@example.test'))`,
		},
		{
			label: "ai_threads for fixture users",
			countSQL: `SELECT COUNT(*) FROM ai_threads WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM ai_threads WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "pii_store for fixture-owned accounts/transactions",
			countSQL: `SELECT COUNT(*) FROM pii_store WHERE
				(entity_type = 'account' AND entity_id IN
					(SELECT id FROM accounts WHERE user_id IN
						(SELECT id FROM users WHERE email LIKE '%@example.test')))
				OR (entity_type = 'transaction' AND entity_id IN
					(SELECT id FROM transactions WHERE user_id IN
						(SELECT id FROM users WHERE email LIKE '%@example.test')))`,
			deleteSQL: `DELETE FROM pii_store WHERE
				(entity_type = 'account' AND entity_id IN
					(SELECT id FROM accounts WHERE user_id IN
						(SELECT id FROM users WHERE email LIKE '%@example.test')))
				OR (entity_type = 'transaction' AND entity_id IN
					(SELECT id FROM transactions WHERE user_id IN
						(SELECT id FROM users WHERE email LIKE '%@example.test')))`,
		},
		{
			label: "transactions for fixture users",
			countSQL: `SELECT COUNT(*) FROM transactions WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM transactions WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "savings_goals for fixture users",
			countSQL: `SELECT COUNT(*) FROM savings_goals WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM savings_goals WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "investments for fixture users",
			countSQL: `SELECT COUNT(*) FROM investments WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM investments WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "budgets for fixture users",
			countSQL: `SELECT COUNT(*) FROM budgets WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM budgets WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "categorization_rules for fixture users",
			countSQL: `SELECT COUNT(*) FROM categorization_rules WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM categorization_rules WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "plaid_items for fixture users",
			countSQL: `SELECT COUNT(*) FROM plaid_items WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM plaid_items WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "accounts for fixture users",
			countSQL: `SELECT COUNT(*) FROM accounts WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM accounts WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "households owned by fixture users",
			countSQL: `SELECT COUNT(*) FROM households WHERE owner_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM households WHERE owner_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label: "sessions for fixture users",
			countSQL: `SELECT COUNT(*) FROM sessions WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
			deleteSQL: `DELETE FROM sessions WHERE user_id IN
				(SELECT id FROM users WHERE email LIKE '%@example.test')`,
		},
		{
			label:     "fixture users (@example.test)",
			countSQL:  `SELECT COUNT(*) FROM users WHERE email LIKE '%@example.test'`,
			deleteSQL: `DELETE FROM users WHERE email LIKE '%@example.test'`,
		},
		// Non-system categories whose names carry the integration-suite
		// timestamp suffix (`HHMMSS.NNNNNN` or `NN-HHMMSS.NNNNNN`). Using
		// SIMILAR TO keeps the match tight so a legitimately-named category
		// like "AlertCat" without a numeric tail is left alone.
		{
			label: "fixture categories (timestamp-suffix, non-system)",
			countSQL: `SELECT COUNT(*) FROM categories
				WHERE is_system = false
				  AND name SIMILAR TO '%[0-9]{6}\.[0-9]{6,}'`,
			deleteSQL: `DELETE FROM categories
				WHERE is_system = false
				  AND name SIMILAR TO '%[0-9]{6}\.[0-9]{6,}'`,
		},
	}

	for _, s := range steps {
		n, err := s.run(ctx, db, *apply)
		if err != nil {
			log.Fatalf("%s: %v", s.label, err)
		}
		verb := "would delete"
		if *apply {
			verb = "deleted"
		}
		fmt.Printf("%6d  %s  — %s\n", n, verb, s.label)
	}

	if !*apply {
		fmt.Println("\n(dry run — re-run with --apply to actually delete)")
	}
}

type cleanupStep struct {
	label     string
	countSQL  string
	deleteSQL string
}

func (s cleanupStep) run(ctx context.Context, db *sql.DB, apply bool) (int64, error) {
	if !apply {
		var n int64
		if err := db.QueryRowContext(ctx, s.countSQL).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	res, err := db.ExecContext(ctx, s.deleteSQL)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// databaseName extracts the database name from a Postgres URL.
func databaseName(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", fmt.Errorf("no database name in URL")
	}
	return name, nil
}
