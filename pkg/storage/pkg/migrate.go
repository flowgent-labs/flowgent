package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationTimeout = 2 * time.Minute

const migrationTableDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT NOT NULL,
	filename TEXT NOT NULL,
	applied_at %s NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (version, filename)
)`

type migrationFile struct {
	version  string
	filename string
	path     string
}

// migrationDirectory locates the SQL shipped beside the binary, or the source
// tree during development. An explicit override must exist; it never falls back.
func migrationDirectory(dbType string) (string, error) {
	if dbType != "postgres" && dbType != "sqlite" {
		return "", fmt.Errorf("unsupported migration database %q", dbType)
	}
	if root := os.Getenv("FLOWGENT_MIGRATION_DIR"); root != "" {
		return filepath.Join(root, dbType), nil
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(executable), "migration", dbType)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "migration", dbType)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return "", fmt.Errorf("migration/%s not found; set FLOWGENT_MIGRATION_DIR", dbType)
}

func migrationFiles(dbType string) ([]migrationFile, error) {
	dir, err := migrationDirectory(dbType)
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	err = fs.WalkDir(os.DirFS(dir), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		version, filename, ok := strings.Cut(path, "/")
		if !ok {
			return fmt.Errorf("SQL file must be under a version directory: %s", path)
		}
		files = append(files, migrationFile{version, filename, filepath.Join(dir, filepath.FromSlash(path))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan migrations %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no SQL migrations in %s", dir)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].version != files[j].version {
			return files[i].version < files[j].version
		}
		return files[i].filename < files[j].filename
	})
	return files, nil
}

func (m migrationFile) read() (string, error) {
	content, err := os.ReadFile(m.path)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", m.path, err)
	}
	return string(content), nil
}

// RunMigrations scans versioned SQLite SQL files and applies only missing ledger
// entries. Each file and its ledger row commit together under a writer lock.
func RunMigrations(db *sql.DB, dbType string) error {
	files, err := migrationFiles(dbType)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(migrationTableDDL, "TEXT")); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, file := range files {
		if err := applySQLiteMigration(ctx, conn, file); err != nil {
			return fmt.Errorf("migration %s/%s: %w", file.version, file.filename, err)
		}
	}
	return nil
}

func applySQLiteMigration(ctx context.Context, conn *sql.Conn, file migrationFile) error {
	// BEGIN IMMEDIATE serializes writers before checking the ledger, including
	// callers using separate database handles or processes.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(rollbackCtx, "ROLLBACK")
	}()
	var applied int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=$1 AND filename=$2`,
		file.version, file.filename).Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		content, err := file.read()
		if err != nil {
			return err
		}
		slog.Info("applying migration", "database", "sqlite", "version", file.version, "file", file.filename)
		if _, err := conn.ExecContext(ctx, content); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO schema_migrations (version,filename,applied_at) VALUES ($1,$2,$3)`,
			file.version, file.filename, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	_, err := conn.ExecContext(ctx, "COMMIT")
	return err
}

// RunMigrationsPG serializes startup migrations for the selected PostgreSQL
// schema. Successful files stay committed if a later file fails.
func RunMigrationsPG(pool *pgxpool.Pool, dbType string) error {
	files, err := migrationFiles(dbType)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	var lockKey int64
	if err := conn.QueryRow(ctx,
		`SELECT hashtextextended(current_database() || ':' || current_schema() || ':flowgent-migrate', 0)`,
	).Scan(&lockKey); err != nil {
		conn.Release()
		return err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		_ = conn.Hijack().Close(context.Background())
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
			_ = conn.Hijack().Close(unlockCtx)
		} else {
			conn.Release()
		}
	}()
	if _, err := conn.Exec(ctx, fmt.Sprintf(migrationTableDDL, "TIMESTAMPTZ")); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, file := range files {
		if err := applyPostgresMigration(ctx, conn, file); err != nil {
			return fmt.Errorf("migration %s/%s: %w", file.version, file.filename, err)
		}
	}
	return nil
}

func applyPostgresMigration(ctx context.Context, conn *pgxpool.Conn, file migrationFile) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var applied int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=$1 AND filename=$2`,
		file.version, file.filename).Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		content, err := file.read()
		if err != nil {
			return err
		}
		slog.Info("applying migration", "database", "postgres", "version", file.version, "file", file.filename)
		// No bind parameters: pgx executes the complete DDL/DML script, including
		// function bodies, without splitting statements on semicolons.
		if _, err := tx.Exec(ctx, content, pgx.QueryExecModeSimpleProtocol); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version,filename,applied_at) VALUES ($1,$2,$3)`,
			file.version, file.filename, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
