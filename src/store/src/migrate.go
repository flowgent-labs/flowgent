package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/store/src/migration"
)

// RunMigrations applies pending SQL migration files from the embedded migration/ data.
// It is idempotent — already-applied files are skipped.
func RunMigrations(db *sql.DB, dbType string) error {
	if err := ensureMigrationTable(db); err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}

	entries, err := fs.ReadDir(migration.FS, dbType)
	if err != nil {
		return fmt.Errorf("read migration dir for %s: %w", dbType, err)
	}

	versions := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)

	for _, ver := range versions {
		verPath := filepath.Join(dbType, ver)
		files, err := fs.ReadDir(migration.FS, verPath)
		if err != nil {
			return fmt.Errorf("read version dir %s: %w", verPath, err)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
				continue
			}

			applied, err := checkApplied(db, ver, f.Name())
			if err != nil {
				return fmt.Errorf("check %s/%s: %w", ver, f.Name(), err)
			}
			if applied {
				continue
			}

			filePath := filepath.Join(verPath, f.Name())
			sqlBytes, err := migration.FS.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("read %s: %w", filePath, err)
			}

			log.Printf("[migrate] applying %s/%s/%s", dbType, ver, f.Name())
			if _, err := db.ExecContext(context.Background(), string(sqlBytes)); err != nil {
				return fmt.Errorf("exec %s: %w", filePath, err)
			}
			if err := recordMigration(db, ver, f.Name()); err != nil {
				return fmt.Errorf("record %s: %w", filePath, err)
			}
		}
	}
	return nil
}

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT NOT NULL,
		filename   TEXT NOT NULL,
		applied_at TEXT NOT NULL,
		PRIMARY KEY (version, filename)
	)`)
	return err
}

func checkApplied(db *sql.DB, version, filename string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(1) FROM schema_migrations WHERE version = $1 AND filename = $2`,
		version, filename,
	).Scan(&count)
	return count > 0, err
}

func recordMigration(db *sql.DB, version, filename string) error {
	_, err := db.Exec(
		`INSERT INTO schema_migrations (version, filename, applied_at) VALUES ($1, $2, $3)`,
		version, filename, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}
