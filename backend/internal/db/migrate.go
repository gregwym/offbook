package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func RunMigrations(databaseURL, migrationsPath string) error {
	hasFiles, err := hasMigrationFiles(migrationsPath)
	if err != nil {
		return err
	}
	if !hasFiles {
		return nil
	}

	sqlDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	driver, err := migratepg.WithInstance(sqlDB, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("migrate postgres driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate.NewWithDatabaseInstance: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func hasMigrationFiles(dir string) (bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return false, fmt.Errorf("glob migrations: %w", err)
	}
	if len(matches) > 0 {
		return true, nil
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, nil
}
