package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations applies pending SQL migration files from the embedded migration/ data.
// It is idempotent — already-applied files are skipped.
func RunMigrations(db *sql.DB, dbType string) error {
	if err := ensureMigrationTable(db); err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}
	if err := renameLegacyTenantColumns(db); err != nil {
		return fmt.Errorf("rename legacy tenant columns: %w", err)
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

			slog.Debug("migrate applying", "dbType", dbType, "version", ver, "file", f.Name())
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

// RunMigrationsPG applies pending SQL migration files against a pgxpool (PostgreSQL).
func RunMigrationsPG(pool *pgxpool.Pool, dbType string) error {
	if err := ensureMigrationTablePG(pool); err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}
	if err := renameLegacyTenantColumnsPG(pool); err != nil {
		return fmt.Errorf("rename legacy tenant columns: %w", err)
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

			applied, err := checkAppliedPG(pool, ver, f.Name())
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

			slog.Debug("migrate applying", "dbType", dbType, "version", ver, "file", f.Name())
			if _, err := pool.Exec(context.Background(), string(sqlBytes)); err != nil {
				return fmt.Errorf("exec %s: %w", filePath, err)
			}
			if err := recordMigrationPG(pool, ver, f.Name()); err != nil {
				return fmt.Errorf("record %s: %w", filePath, err)
			}
		}
	}
	return nil
}

// renameLegacyTenantColumns renames tenant_id → namespace_id in every table
// that still carries the old name. Idempotent — if the column is already
// namespace_id, the ALTER TABLE is a no-op (SQLite returns an error we ignore).
func renameLegacyTenantColumns(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		tables = append(tables, t)
	}

	for _, t := range tables {
		cols, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", t))
		if err != nil {
			continue
		}
		var hasTenantID bool
		var hasNamespaceID bool
		for cols.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dflt sql.NullString
			var pk int
			if err := cols.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				continue
			}
			if name == "tenant_id" {
				hasTenantID = true
			}
			if name == "namespace_id" {
				hasNamespaceID = true
			}
		}
		cols.Close()
		if hasTenantID && !hasNamespaceID {
			_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN tenant_id TO namespace_id", t))
		}
	}
	return nil
}

// renameLegacyTenantColumnsPG is the PostgreSQL equivalent of renameLegacyTenantColumns.
func renameLegacyTenantColumnsPG(pool *pgxpool.Pool) error {
	rows, err := pool.Query(context.Background(), `
		SELECT table_name FROM information_schema.columns
		WHERE column_name = 'tenant_id'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		tables = append(tables, t)
	}
	for _, t := range tables {
		var hasNs bool
		_ = pool.QueryRow(context.Background(), `
			SELECT count(1) FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'namespace_id'`, t).Scan(&hasNs)
		if !hasNs {
			_, _ = pool.Exec(context.Background(),
				fmt.Sprintf("ALTER TABLE %s RENAME COLUMN tenant_id TO namespace_id", t))
		}
	}
	return nil
}

func ensureMigrationTablePG(pool *pgxpool.Pool) error {
	_, err := pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT NOT NULL,
		filename   TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (version, filename)
	)`)
	return err
}

func checkAppliedPG(pool *pgxpool.Pool, version, filename string) (bool, error) {
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(1) FROM schema_migrations WHERE version = $1 AND filename = $2`,
		version, filename,
	).Scan(&count)
	return count > 0, err
}

func recordMigrationPG(pool *pgxpool.Pool, version, filename string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO schema_migrations (version, filename, applied_at) VALUES ($1, $2, $3)`,
		version, filename, time.Now().UTC().Format(time.RFC3339),
	)
	return err
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
