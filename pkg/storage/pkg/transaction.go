package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLTransaction is a small dialect adapter for protocols shared by SQLite and
// PostgreSQL. Repositories retain ownership of their SQL and authorization.
type SQLTransaction struct {
	sqlite   *sql.Tx
	postgres pgx.Tx
}
type SQLRow interface{ Scan(...any) error }

func (t *SQLTransaction) QueryRow(ctx context.Context, q string, args ...any) SQLRow {
	if t.sqlite != nil {
		return t.sqlite.QueryRowContext(ctx, q, args...)
	}
	return t.postgres.QueryRow(ctx, postgresParameters(q), args...)
}
func (t *SQLTransaction) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	if t.sqlite != nil {
		r, e := t.sqlite.ExecContext(ctx, q, args...)
		if e != nil {
			return 0, e
		}
		return r.RowsAffected()
	}
	r, e := t.postgres.Exec(ctx, postgresParameters(q), args...)
	return r.RowsAffected(), e
}
func (t *SQLTransaction) LockSuffix() string {
	if t.postgres != nil {
		return " FOR UPDATE"
	}
	return ""
}
func postgresParameters(q string) string { i := 0; return replaceParameters(q, &i) }
func replaceParameters(q string, i *int) string {
	// These repository-owned statements never use question marks inside literals.
	var b strings.Builder
	for _, r := range q {
		if r == '?' {
			*i++
			fmt.Fprintf(&b, "$%d", *i)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func WithSQLTransaction(ctx context.Context, db any, fn func(*SQLTransaction) error) error {
	switch d := db.(type) {
	case *sql.DB:
		tx, e := d.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = fn(&SQLTransaction{sqlite: tx}); e != nil {
			return e
		}
		return tx.Commit()
	case *pgxpool.Pool:
		tx, e := d.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		if e = fn(&SQLTransaction{postgres: tx}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	default:
		return fmt.Errorf("unsupported SQL database %T", db)
	}
}

// WrapSQLTransaction allows existing entity transactions to append an outbox
// record without changing their persistence or commit boundaries.
func WrapSQLTransaction(tx any) *SQLTransaction {
	switch t := tx.(type) {
	case *sql.Tx:
		return &SQLTransaction{sqlite: t}
	case pgx.Tx:
		return &SQLTransaction{postgres: t}
	default:
		panic("unsupported transaction")
	}
}

func IsNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) }
