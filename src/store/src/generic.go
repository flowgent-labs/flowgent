package store

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Generic CRUD (reflection-based) ─────────────────────

// PGGet fetches a single entity by ID using pgx.RowToStructByName.
func PGGet[T any](ctx context.Context, pool *pgxpool.Pool, table, idCol, id string) (*T, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf("SELECT * FROM %s WHERE %s=$1 LIMIT 1", table, idCol), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entity, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("%s not found: %s=%s: %w", table, idCol, id, err)
	}
	return entity, nil
}

// PGSelect fetches a page of entities ordered by created_at DESC.
func PGSelect[T any](ctx context.Context, pool *pgxpool.Pool, table string, offset, limit int) ([]*T, error) {
	rows, err := pool.Query(ctx, fmt.Sprintf("SELECT * FROM %s ORDER BY created_at DESC LIMIT $1 OFFSET $2", table), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[T])
}

// PGSave inserts or updates an entity using reflection to build column lists.
// Uses INSERT ... ON CONFLICT DO UPDATE for upsert semantics.
func PGSave[T any](ctx context.Context, pool *pgxpool.Pool, table, idCol, idVal string, entity *T) error {
	cols, vals := structFields(entity)
	if len(cols) == 0 {
		return fmt.Errorf("no fields on %T", entity)
	}

	placeholders := make([]string, len(cols))
	updates := make([]string, len(cols))
	args := make([]any, len(cols))
	for i := range cols {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		updates[i] = fmt.Sprintf("%s=EXCLUDED.%s", cols[i], cols[i])
		args[i] = vals[i]
	}

	sql := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		table, strings.Join(cols, ","), strings.Join(placeholders, ","), idCol, strings.Join(updates, ","),
	)
	_, err := pool.Exec(ctx, sql, args...)
	return err
}

// PGDelete soft-deletes an entity by ID.
func PGDelete(ctx context.Context, pool *pgxpool.Pool, table, idCol, id string) error {
	_, err := pool.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=true, status='DELETED', updated_at=$1 WHERE %s=$2", table, idCol),
		time.Now(), id)
	return err
}

// SQGet fetches a single entity by ID using reflection scanning (SQLite).
func SQGet[T any](ctx context.Context, conn *sql.DB, table, idCol, id string) (*T, error) {
	row := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT * FROM %s WHERE %s=?1 LIMIT 1", table, idCol), id)
	var entity T
	if err := scanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("%s not found: %s=%s: %w", table, idCol, id, err)
	}
	return &entity, nil
}

// SQSelect fetches a page of entities (SQLite).
func SQSelect[T any](ctx context.Context, conn *sql.DB, table string, offset, limit int) ([]T, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s ORDER BY created_at DESC LIMIT ?1 OFFSET ?2", table), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var entity T
		if err := scanStruct(rows, &entity); err != nil {
			continue
		}
		out = append(out, entity)
	}
	return out, nil
}

// ─── Reflection helpers ──────────────────────────────────

// structFields extracts column names and values from a struct's db/json tags.
func structFields(entity any) (cols []string, vals []any) {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		col := columnName(f)
		if col == "" || col == "-" {
			continue
		}
		cols = append(cols, col)
		vals = append(vals, v.Field(i).Interface())
	}
	return
}

func columnName(f reflect.StructField) string {
	if tag := f.Tag.Get("db"); tag != "" {
		return strings.Split(tag, ",")[0]
	}
	if tag := f.Tag.Get("json"); tag != "" {
		n := strings.Split(tag, ",")[0]
		if n != "" && n != "-" {
			return n
		}
	}
	return toSnakeCase(f.Name)
}

func scanStruct(scanner interface{ Scan(dest ...any) error }, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be *struct")
	}
	ev := v.Elem()
	t := ev.Type()

	var fields []int
	var ptrs []any
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		fields = append(fields, i)
		ptrs = append(ptrs, ev.Field(i).Addr().Interface())
	}
	if len(ptrs) == 0 {
		return fmt.Errorf("no exported fields")
	}
	return scanner.Scan(ptrs...)
}

func toSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
