package db_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// TestMigrations_UpDownUpIsIdempotent walks every migration version N from 1
// to the latest and asserts that the round-trip up(N) → down(N-1) → up(N)
// produces a schema identical to the first up(N). A broken down-migration
// (forgetting a DROP, leaving a stale index) surfaces as a snapshot diff.
//
// As a final invariant, after applying down-all from latest back to 0 the
// only public-schema table left must be schema_migrations, and it must be
// empty.
//
// The test runs against an ephemeral database created next to the
// DATABASE_URL target — the prod/dev DB is never touched. Requires a role
// with CREATEDB; the test skips with a clear message if the privilege is
// missing.
//
// Schema-only snapshot: column types, nullability, defaults + index
// definitions. Seed-data migrations (000002 categories, 000005
// plaid_category_map) are exercised by the round trip but their rows aren't
// snapshotted — schema reversibility is what we're testing here. Data
// integrity is covered by the regular integration suite.
func TestMigrations_UpDownUpIsIdempotent(t *testing.T) {
	loadRepoDotenv(t)
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("DATABASE_URL")
	}
	if baseURL == "" {
		t.Skip("no DATABASE_URL set; skipping migration round-trip test")
	}

	migrationsPath, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	versions, err := listMigrationVersions(migrationsPath)
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(versions) == 0 {
		t.Fatalf("no migrations found at %s", migrationsPath)
	}

	ephemeral, cleanup := createEphemeralDB(t, baseURL)
	defer cleanup()

	m, closeFn := newMigrator(t, ephemeral, migrationsPath)
	defer closeFn()

	snapshots := make(map[uint]schemaSnapshot, len(versions))
	for _, v := range versions {
		mustMigrateTo(t, m, v)
		snapshots[v] = takeSnapshot(t, ephemeral)
	}

	for i, v := range versions {
		// Step the most recent up to v fully clear: down to the previous
		// version (or 0 if v is the very first), then up to v again, and
		// assert the schema is byte-identical to the first capture.
		var prev int
		if i == 0 {
			prev = 0
		} else {
			prev = int(versions[i-1])
		}
		mustMigrateTo(t, m, uint(prev))
		mustMigrateTo(t, m, v)

		got := takeSnapshot(t, ephemeral)
		if diff := snapshots[v].diff(got); diff != "" {
			t.Errorf("migration %d not idempotent under up→down→up:\n%s", v, diff)
		}
	}

	// Final invariant: down-all from latest leaves nothing but the
	// schema_migrations bookkeeping table, with zero rows.
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("down-all: %v", err)
	}
	assertOnlySchemaMigrations(t, ephemeral)
}

// --- helpers ---

func loadRepoDotenv(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for i := 0; i < 8; i++ {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// listMigrationVersions returns the sorted unique version numbers parsed from
// the *.up.sql filenames in dir.
func listMigrationVersions(dir string) ([]uint, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[uint]struct{})
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// Filename pattern: NNNNNN_slug.up.sql
		under := strings.Index(name, "_")
		if under <= 0 {
			continue
		}
		var v uint
		if _, err := fmt.Sscanf(name[:under], "%d", &v); err != nil {
			continue
		}
		seen[v] = struct{}{}
	}
	out := make([]uint, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// createEphemeralDB issues CREATE DATABASE against the same Postgres
// instance referenced by baseURL and returns a URL pointing at the new DB
// plus a cleanup that drops it. If the role lacks CREATEDB privilege the
// test is skipped rather than failed — local devs may have a locked-down
// role even if CI doesn't.
func createEphemeralDB(t *testing.T, baseURL string) (string, func()) {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	if u.Path == "" || u.Path == "/" {
		t.Fatalf("DATABASE_URL has no database name: %s", baseURL)
	}
	adminURL := *u
	// Connect to the maintenance database "postgres" to create our scratch
	// DB. This avoids "database in use" issues if the original db has open
	// connections elsewhere.
	adminURL.Path = "/postgres"

	admin, err := sql.Open("postgres", adminURL.String())
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		t.Skipf("admin db ping failed: %v; skipping", err)
	}

	dbName := fmt.Sprintf("offbook_migtest_%d", time.Now().UnixNano())
	if _, err := admin.Exec(fmt.Sprintf("CREATE DATABASE %q", dbName)); err != nil {
		// permission_denied → role lacks CREATEDB; treat as skip, not fail.
		if strings.Contains(err.Error(), "permission denied") {
			t.Skipf("role lacks CREATEDB privilege: %v", err)
		}
		t.Fatalf("create ephemeral db: %v", err)
	}

	target := *u
	target.Path = "/" + dbName
	cleanup := func() {
		adm, err := sql.Open("postgres", adminURL.String())
		if err != nil {
			t.Logf("cleanup: reopen admin: %v", err)
			return
		}
		defer func() { _ = adm.Close() }()
		// Force-drop in case any test goroutine left a connection open.
		if _, err := adm.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", dbName)); err != nil {
			t.Logf("cleanup: drop %s: %v", dbName, err)
		}
	}
	return target.String(), cleanup
}

func newMigrator(t *testing.T, databaseURL, migrationsPath string) (*migrate.Migrate, func()) {
	t.Helper()
	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("migrate driver: %v", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("migrate.New: %v", err)
	}
	return m, func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			t.Logf("migrator source close: %v", srcErr)
		}
		if dbErr != nil {
			t.Logf("migrator db close: %v", dbErr)
		}
	}
}

func mustMigrateTo(t *testing.T, m *migrate.Migrate, v uint) {
	t.Helper()
	var err error
	if v == 0 {
		err = m.Down()
	} else {
		err = m.Migrate(v)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate to %d: %v", v, err)
	}
}

// schemaSnapshot is the canonical-form schema we compare across round trips.
// Each entry is one line; equality reduces to string equality.
type schemaSnapshot struct {
	columns []string
	indexes []string
}

func (s schemaSnapshot) diff(other schemaSnapshot) string {
	var b strings.Builder
	if d := lineDiff("columns", s.columns, other.columns); d != "" {
		b.WriteString(d)
	}
	if d := lineDiff("indexes", s.indexes, other.indexes); d != "" {
		b.WriteString(d)
	}
	return b.String()
}

func lineDiff(label string, want, got []string) string {
	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	var missing, extra []string
	for w := range wantSet {
		if _, ok := gotSet[w]; !ok {
			missing = append(missing, w)
		}
	}
	for g := range gotSet {
		if _, ok := wantSet[g]; !ok {
			extra = append(extra, g)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	sort.Strings(missing)
	sort.Strings(extra)
	var b strings.Builder
	fmt.Fprintf(&b, "  %s diff:\n", label)
	for _, m := range missing {
		fmt.Fprintf(&b, "    -%s\n", m)
	}
	for _, e := range extra {
		fmt.Fprintf(&b, "    +%s\n", e)
	}
	return b.String()
}

func takeSnapshot(t *testing.T, databaseURL string) schemaSnapshot {
	t.Helper()
	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("snapshot open: %v", err)
	}
	defer func() { _ = conn.Close() }()

	const colQuery = `
		SELECT table_name, column_name, data_type, is_nullable,
		       COALESCE(column_default, '')
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name <> 'schema_migrations'
		 ORDER BY table_name, ordinal_position`
	rows, err := conn.Query(colQuery)
	if err != nil {
		t.Fatalf("snapshot columns: %v", err)
	}
	var cols []string
	for rows.Next() {
		var table, col, typ, nullable, def string
		if err := rows.Scan(&table, &col, &typ, &nullable, &def); err != nil {
			t.Fatalf("scan column row: %v", err)
		}
		cols = append(cols, fmt.Sprintf("%s.%s %s nullable=%s default=%s", table, col, typ, nullable, def))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("columns rows.Err: %v", err)
	}
	_ = rows.Close()

	const idxQuery = `
		SELECT tablename, indexname, indexdef
		  FROM pg_indexes
		 WHERE schemaname = 'public'
		   AND tablename  <> 'schema_migrations'
		 ORDER BY tablename, indexname`
	irows, err := conn.Query(idxQuery)
	if err != nil {
		t.Fatalf("snapshot indexes: %v", err)
	}
	var idxs []string
	for irows.Next() {
		var table, idx, def string
		if err := irows.Scan(&table, &idx, &def); err != nil {
			t.Fatalf("scan index row: %v", err)
		}
		idxs = append(idxs, fmt.Sprintf("%s.%s := %s", table, idx, def))
	}
	if err := irows.Err(); err != nil {
		t.Fatalf("indexes rows.Err: %v", err)
	}
	_ = irows.Close()

	return schemaSnapshot{columns: cols, indexes: idxs}
}

// assertOnlySchemaMigrations verifies that after a full down-all the public
// schema has exactly one table — schema_migrations — and it has no rows.
func assertOnlySchemaMigrations(t *testing.T, databaseURL string) {
	t.Helper()
	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("post-down open: %v", err)
	}
	defer func() { _ = conn.Close() }()

	rows, err := conn.Query(`SELECT table_name FROM information_schema.tables
	                          WHERE table_schema = 'public' ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list tables after down-all: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tables rows.Err: %v", err)
	}
	_ = rows.Close()

	if len(tables) != 1 || tables[0] != "schema_migrations" {
		t.Fatalf("after down-all want only schema_migrations, got %v", tables)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 0 {
		t.Fatalf("schema_migrations row count = %d, want 0", count)
	}
}
