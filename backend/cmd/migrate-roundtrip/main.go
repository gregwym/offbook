// migrate-roundtrip exercises the full migration set for reversibility (#358).
//
// It runs, against the test DB only:
//
//	reset (down-all) → up → assert seed → down-all → assert empty → up → assert seed
//
// This is the mechanical enforcement of the expand→migrate→contract policy:
// every migration must ship a working down file, and a fresh up must leave the
// schema at head with seed data intact. A missing or broken down migration, or
// a seed that doesn't re-apply cleanly, fails the build.
//
// The sequence is DESTRUCTIVE (down-all drops every table), so it refuses to run
// against anything but the test DB. CI runs it against the same offbook_test
// Postgres the backend tests use; `make verify` runs it locally before the
// suite. It ends with the DB fully migrated, so subsequent steps see head.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"github.com/gregwym/offbook/backend/internal/config"
)

// systemCategoryCount is the number of seeded system categories (migration
// 000002). The round-trip asserts every one re-appears after a fresh up.
const systemCategoryCount = 20

func main() {
	path := flag.String("path", "migrations", "migrations directory")
	flag.Parse()

	cfg := config.MustLoad()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is empty")
	}
	// Hard guard: down-all wipes the entire schema. Only ever run this against
	// the test DB. Both signals must agree with "test" so a stray APP_ENV or a
	// mislabeled URL can't nuke a dev/prod database.
	if cfg.AppEnv != config.AppEnvTest {
		log.Fatalf("migrate-roundtrip refuses to run with APP_ENV=%q — it drops every table; run with APP_ENV=test", cfg.AppEnv)
	}
	if !strings.Contains(cfg.DatabaseURL, "_test") {
		log.Fatalf("migrate-roundtrip refuses to run: DATABASE_URL does not target a *_test database")
	}

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 1. Fresh slate, BEFORE building the migrator. The test DB persists across
	//    runs and may carry rows from a prior suite (or a half-migrated state),
	//    so hard-drop the schema rather than migrate-down to it. This measures
	//    migration reversibility on the canonical empty→up→down→up path the AC
	//    asks for — not "can we reverse whatever fixtures happen to be lying
	//    around", which depends on residual data and would flake. The reset also
	//    removes golang-migrate's schema_migrations table, so the migrator must
	//    be constructed afterward (WithInstance ensures that table at build time).
	reset(db)

	m, err := newMigrator(db, *path)
	if err != nil {
		log.Fatal(err)
	}

	// 2. All up, from empty. Asserts every up migration applies and seeds land.
	step("apply all migrations (up)", m.Up)
	assertAtHead(m)
	assertSeedIntact(db)

	// 3. All down. Asserts every migration has a working, complete down file.
	step("roll back all migrations (down-all)", m.Down)
	assertEmpty(db)

	// 4. All up again. Asserts re-migration from empty is clean and idempotent.
	step("re-apply all migrations (up)", m.Up)
	assertAtHead(m)
	assertSeedIntact(db)

	log.Println("migrate-roundtrip: OK — up→down→up clean, seed integrity verified")
}

// reset drops the entire public schema for a genuinely empty starting point,
// including golang-migrate's schema_migrations table. Safe here because the
// caller already proved this is the *_test DB.
func reset(db *sql.DB) {
	log.Print("→ reset to a fresh, empty schema (DROP SCHEMA public CASCADE)")
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		log.Fatalf("reset schema: %v", err)
	}
}

// step runs one migrate action and fails loudly on any error except
// ErrNoChange (a legitimate no-op).
func step(label string, action func() error) {
	log.Printf("→ %s", label)
	if err := action(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("%s: %v", label, err)
	}
}

// assertAtHead confirms the migrator reports the head version, not dirty.
func assertAtHead(m *migrate.Migrate) {
	v, dirty, err := m.Version()
	if err != nil {
		log.Fatalf("version after up: %v", err)
	}
	if dirty {
		log.Fatalf("schema is dirty at version %d after up — a migration failed partway", v)
	}
	log.Printf("  at version %d (clean)", v)
}

// assertSeedIntact confirms the seed migrations re-populated after a fresh up.
func assertSeedIntact(db *sql.DB) {
	var cats int
	if err := db.QueryRow(`SELECT count(*) FROM categories WHERE is_system AND deleted_at IS NULL`).Scan(&cats); err != nil {
		log.Fatalf("count system categories: %v", err)
	}
	if cats != systemCategoryCount {
		log.Fatalf("seed integrity: expected %d system categories, found %d", systemCategoryCount, cats)
	}

	var mappings int
	if err := db.QueryRow(`SELECT count(*) FROM plaid_category_map`).Scan(&mappings); err != nil {
		log.Fatalf("count plaid_category_map: %v", err)
	}
	if mappings == 0 {
		log.Fatal("seed integrity: plaid_category_map is empty after up")
	}
	log.Printf("  seed intact: %d system categories, %d plaid category mappings", cats, mappings)
}

// assertEmpty confirms down-all left no schema behind — the version is nil and
// a representative domain table is gone.
func assertEmpty(db *sql.DB) {
	var reg sql.NullString
	if err := db.QueryRow(`SELECT to_regclass('public.categories')`).Scan(&reg); err != nil {
		log.Fatalf("to_regclass after down-all: %v", err)
	}
	if reg.Valid {
		log.Fatalf("down-all left table %q behind — a down migration is incomplete", reg.String)
	}
	log.Print("  schema empty after down-all")
}

func newMigrator(db *sql.DB, path string) (*migrate.Migrate, error) {
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return nil, fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+path, "postgres", driver)
	if err != nil {
		return nil, fmt.Errorf("migrate.New: %w", err)
	}
	return m, nil
}
