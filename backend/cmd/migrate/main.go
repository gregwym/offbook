// Migration CLI built into the repo. Use instead of a system `migrate` install.
//
//	go run ./cmd/migrate up             # apply all pending migrations
//	go run ./cmd/migrate down           # roll back one step
//	go run ./cmd/migrate down-all       # drop everything (dev only)
//	go run ./cmd/migrate version        # print current version
//	go run ./cmd/migrate force <ver>    # mark version <ver> as clean (recovery)
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"github.com/gregwym/offbook/backend/internal/config"
)

func main() {
	path := flag.String("path", "migrations", "migrations directory")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is empty")
	}

	m, closeFn, err := newMigrator(cfg.DatabaseURL, *path)
	if err != nil {
		log.Fatal(err)
	}
	defer closeFn()

	switch args[0] {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("up: %v", err)
		}
		log.Println("up: OK")
	case "down":
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("down: %v", err)
		}
		log.Println("down: OK")
	case "down-all":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("down-all: %v", err)
		}
		log.Println("down-all: OK")
	case "version":
		v, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("no migrations applied")
			return
		}
		if err != nil {
			log.Fatalf("version: %v", err)
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
	case "force":
		if len(args) < 2 {
			log.Fatal("force requires a version argument")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			log.Fatalf("invalid version: %v", err)
		}
		if err := m.Force(v); err != nil {
			log.Fatalf("force: %v", err)
		}
		log.Printf("force %d: OK", v)
	default:
		usage()
		os.Exit(2)
	}
}

func newMigrator(databaseURL, path string) (*migrate.Migrate, func(), error) {
	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("sql.Open: %w", err)
	}
	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	if err != nil {
		sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+path, "postgres", driver)
	if err != nil {
		sqlDB.Close()
		return nil, nil, fmt.Errorf("migrate.New: %w", err)
	}
	return m, func() { sqlDB.Close() }, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate [-path <dir>] {up|down|down-all|version|force <ver>}")
}
