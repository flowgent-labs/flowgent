package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a shared pgxpool.
func NewPostgresPool(ctx context.Context, dsn, schema string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil { panic(fmt.Sprintf("pg: %v", err)) }
	cfg.MaxConns, cfg.MinConns = 20, 2
	if schema == "" { schema = "public" }
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil { panic(fmt.Sprintf("pg connect: %v", err)) }
	if err := pool.Ping(ctx); err != nil { panic(fmt.Sprintf("pg ping: %v", err)) }
	return pool
}

// PostgresGenericStore provides reflection-based generic CRUD for entity type T.
// SQL is built from struct tags (db > json > snake_case) with parameterized placeholders ($1, $2...).
// Save uses INSERT ON CONFLICT for idempotent upsert.
type PostgresGenericStore[T any] struct {
	Pool  *pgxpool.Pool
	Table string
	IDCol string
}

func (s *PostgresGenericStore[T]) Get(ctx context.Context, id string) (*T, error) {
	cols := s.columns()
	if err := validateIdent(s.Table, s.IDCol); err != nil { return nil, err }
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s=$1 LIMIT 1", cols, s.Table, s.IDCol), id)
	if err != nil { return nil, err }
	defer rows.Close()
	if !rows.Next() { return nil, fmt.Errorf("%s not found: %s=%s", s.Table, s.IDCol, id) }
	var entity T
	if err := scanTaggedStruct(rows, &entity); err != nil { return nil, err }
	return &entity, nil
}

func (s *PostgresGenericStore[T]) Select(ctx context.Context, offset, limit int) ([]*T, error) {
	cols := s.columns()
	if err := validateIdent(s.Table); err != nil { return nil, err }
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM %s ORDER BY created_at DESC LIMIT $1 OFFSET $2", cols, s.Table), limit, offset)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []*T
	for rows.Next() {
		entity := new(T)
		if err := scanTaggedStruct(rows, entity); err != nil { continue }
		out = append(out, entity)
	}
	return out, nil
}

func (s *PostgresGenericStore[T]) columns() string {
	var entity T
	cols, _ := structFields(&entity)
	return strings.Join(cols, ",")
}

// scanTaggedStruct scans a row into a struct, handling JSON/complex types automatically.
func scanTaggedStruct(row pgx.Row, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be *struct")
	}
	ev := v.Elem()
	t := ev.Type()
	ptrs := make([]any, t.NumField())
	jsonFields := make(map[int]bool)
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() { continue }
		fv := ev.Field(i)
		ft := fv.Type()
		if isJSONType(ft) {
			jsonFields[i] = true
			ptrs[i] = reflect.New(reflect.TypeOf([]byte{})).Interface()
		} else {
			ptrs[i] = fv.Addr().Interface()
		}
	}
	if err := row.Scan(ptrs...); err != nil { return err }
	for fi := range jsonFields {
		b := ptrs[fi].(*[]byte)
		if b == nil || len(*b) == 0 { continue }
		if err := json.Unmarshal(*b, ev.Field(fi).Addr().Interface()); err != nil {
			continue
		}
	}
	return nil
}

func isJSONType(ft reflect.Type) bool {
	if ft == reflect.TypeOf(json.RawMessage{}) { return true }
	switch ft.Kind() {
	case reflect.Map, reflect.Slice:
		return true
	case reflect.Struct:
		return ft != reflect.TypeOf(time.Time{})
	case reflect.Ptr:
		if ft.Elem().Kind() == reflect.Struct && ft.Elem() != reflect.TypeOf(time.Time{}) { return true }
	}
	return false
}


func (s *PostgresGenericStore[T]) Save(ctx context.Context, entity *T) error {
	cols, args := structFields(entity)
	if len(cols) == 0 { return fmt.Errorf("no fields on %T", entity) }
	holders := make([]string, len(cols))
	updates := make([]string, len(cols))
	for i, c := range cols {
		holders[i] = fmt.Sprintf("$%d", i+1)
		updates[i] = fmt.Sprintf("%s=EXCLUDED.%s", c, c)
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		s.Table, strings.Join(cols, ","), strings.Join(holders, ","), s.IDCol, strings.Join(updates, ","))
	_, err := s.Pool.Exec(ctx, sql, args...)
	return err
}

func (s *PostgresGenericStore[T]) Delete(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=true, status='DELETED', updated_at=NOW() WHERE %s=$1", s.Table, s.IDCol), id)
	return err
}

func (s *PostgresGenericStore[T]) Exec(ctx context.Context, sql string, params ...any) (int64, error) {
	tag, err := s.Pool.Exec(ctx, sql, params...)
	if err != nil { return 0, err }
	return tag.RowsAffected(), nil
}

func StructFields(entity any) ([]string, []any) { return structFields(entity) }

func structFields(entity any) (cols []string, args []any) {
	v := reflect.ValueOf(entity)
	if v.Kind() == reflect.Ptr { v = v.Elem() }
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() { continue }
		col := colName(f)
		if col == "" || col == "-" { continue }
		cols = append(cols, col)
		args = append(args, v.Field(i).Interface())
	}
	return
}

func colName(f reflect.StructField) string {
	if tag := f.Tag.Get("db"); tag != "" { return strings.Split(tag, ",")[0] }
	if tag := f.Tag.Get("json"); tag != "" {
		n := strings.Split(tag, ",")[0]
		if n != "" && n != "-" { return n }
	}
	return toSnake(f.Name)
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validateIdent(names ...string) error {
	for _, n := range names {
		if !identRe.MatchString(n) {
			return fmt.Errorf("invalid SQL identifier: %q", n)
		}
	}
	return nil
}


func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' { b.WriteByte('_') }
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
