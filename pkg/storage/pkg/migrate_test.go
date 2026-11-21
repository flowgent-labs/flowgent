package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationFixture(t *testing.T, dbType string, scripts map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range scripts {
		writeMigration(t, filepath.Join(root, dbType, name), content)
	}
	t.Setenv("FLOWGENT_MIGRATION_DIR", root)
	return root
}

func writeMigration(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func migrationDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(10000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrationsResumeInVersionAndFilenameOrder(t *testing.T) {
	root := migrationFixture(t, "sqlite", map[string]string{
		"002/02_update.dml.sql": `UPDATE example SET value=value+10;`,
		"001/01_init.ddl.sql":   `CREATE TABLE example(value INTEGER); INSERT INTO example VALUES(1);`,
		"002/01_insert.dml.sql": `INSERT INTO example VALUES(2);`,
	})
	db := migrationDB(t, filepath.Join(t.TempDir(), "test.db"))
	// The old SQLite ledger had no applied_at default. Keep it compatible.
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT NOT NULL, filename TEXT NOT NULL,
		applied_at TEXT NOT NULL, PRIMARY KEY(version,filename))`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RunMigrations(db, "sqlite"); err != nil {
			t.Fatal(err)
		}
	}
	writeMigration(t, filepath.Join(root, "sqlite/003/01_next.ddl.sql"),
		`ALTER TABLE example ADD COLUMN note TEXT; UPDATE example SET note='new';`)
	if err := RunMigrations(db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	var sum, count int
	if err := db.QueryRow(`SELECT SUM(value),COUNT(*) FROM example WHERE note='new'`).Scan(&sum, &count); err != nil {
		t.Fatal(err)
	}
	if sum != 23 || count != 2 {
		t.Fatalf("scripts repeated or ran out of order: sum=%d count=%d", sum, count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("ledger count=%d err=%v", count, err)
	}
}

func TestMigrationsRollbackFailedFileAndResume(t *testing.T) {
	root := migrationFixture(t, "sqlite", map[string]string{
		"001/01_init.sql": `CREATE TABLE example(value INTEGER); INSERT INTO example VALUES(1);`,
		"002/01_bad.sql":  `INSERT INTO example VALUES(2); CREATE TABLE partial(value INTEGER); INVALID SQL;`,
	})
	db := migrationDB(t, filepath.Join(t.TempDir(), "test.db"))
	if err := RunMigrations(db, "sqlite"); err == nil {
		t.Fatal("expected migration failure")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM example`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed DML persisted: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='partial'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed DDL persisted: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed script recorded or successful script lost: count=%d err=%v", count, err)
	}
	writeMigration(t, filepath.Join(root, "sqlite/002/01_bad.sql"), `INSERT INTO example VALUES(2);`)
	if err := RunMigrations(db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM example`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("resume failed: count=%d err=%v", count, err)
	}
}

func TestMigrationsConcurrentStartup(t *testing.T) {
	migrationFixture(t, "sqlite", map[string]string{
		"001/01_init.sql":   `CREATE TABLE example(value INTEGER); INSERT INTO example VALUES(1);`,
		"002/01_update.sql": `UPDATE example SET value=value+1;`,
	})
	path := filepath.Join(t.TempDir(), "test.db")
	dbs := []*sql.DB{migrationDB(t, path), migrationDB(t, path), migrationDB(t, path)}
	var wg sync.WaitGroup
	for _, db := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RunMigrations(db, "sqlite"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var value int
	if err := dbs[0].QueryRow(`SELECT value FROM example`).Scan(&value); err != nil || value != 2 {
		t.Fatalf("concurrent scripts repeated: value=%d err=%v", value, err)
	}
}

func TestMigrationsMissingDirectoryStopsStartup(t *testing.T) {
	t.Setenv("FLOWGENT_MIGRATION_DIR", filepath.Join(t.TempDir(), "missing"))
	db := migrationDB(t, filepath.Join(t.TempDir(), "test.db"))
	if err := RunMigrations(db, "sqlite"); err == nil {
		t.Fatal("missing SQL must not fall back to repository migrations")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='schema_migrations'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid migration path changed database: count=%d err=%v", count, err)
	}
}

func TestMigrationsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	root := migrationFixture(t, "postgres", map[string]string{
		"001/01_init.sql": `CREATE TABLE example(value INTEGER); INSERT INTO example VALUES(1);`,
		"002/01_bad.sql":  `INSERT INTO example VALUES(2); CREATE TABLE partial(value INTEGER); INVALID SQL;`,
	})
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var suffix uint32
	if err := admin.QueryRow(ctx, `SELECT (random()*1000000000)::bigint`).Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("migration_test_%d", suffix)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	pool := NewPostgresPool(ctx, dsn, schema)
	defer pool.Close()
	if err := RunMigrationsPG(pool, "postgres"); err == nil {
		t.Fatal("expected script failure")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM example`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed DML persisted: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("incorrect ledger after failure: count=%d err=%v", count, err)
	}
	writeMigration(t, filepath.Join(root, "postgres/002/01_bad.sql"), `INSERT INTO example VALUES(2);`)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := RunMigrationsPG(pool, "postgres"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM example`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("concurrent resume failed: count=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("incorrect ledger after resume: count=%d err=%v", count, err)
	}
}
