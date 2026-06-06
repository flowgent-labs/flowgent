package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	_ "modernc.org/sqlite"
)

func NewSQLiteConn(ctx context.Context, dir string) *sql.DB {
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Sprintf("sqlite dir: %v", err))
	}
	dbPath := filepath.Join(dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil { panic(fmt.Sprintf("sqlite open: %v", err)) }
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := RunMigrations(db, "sqlite"); err != nil {
		panic(fmt.Sprintf("sqlite migrate: %v", err))
	}
	return db
}

type SQLiteGenericStore[T any] struct {
	Conn  *sql.DB
	Table string
	IDCol string
}

func (s *SQLiteGenericStore[T]) Get(ctx context.Context, id string) (*T, error) {
	row := s.Conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE %s=?1 AND del_flag=false LIMIT 1", s.Table, s.IDCol), id)
	var entity T
	if err := scanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Table, err)
	}
	return &entity, nil
}

func (s *SQLiteGenericStore[T]) Select(ctx context.Context, offset, limit int) ([]*T, error) {
	rows, err := s.Conn.QueryContext(ctx,
		fmt.Sprintf("SELECT * FROM %s ORDER BY created_at DESC LIMIT ?1 OFFSET ?2", s.Table), limit, offset)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []*T
	for rows.Next() {
		e := new(T)
		if err := scanStruct(rows, e); err != nil { continue }
		out = append(out, e)
	}
	return out, nil
}

func (s *SQLiteGenericStore[T]) Save(ctx context.Context, entity *T) error {
	cols, args := structFields(entity)
	if len(cols) == 0 { return fmt.Errorf("no fields") }
	holders := make([]string, len(cols))
	for i := range cols { holders[i] = "?" }
	_, err := s.Conn.ExecContext(ctx,
		fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
			s.Table, strings.Join(cols, ","), strings.Join(holders, ",")), args...)
	return err
}

func (s *SQLiteGenericStore[T]) Delete(ctx context.Context, id string) error {
	_, err := s.Conn.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=1, status='DELETED', updated_at=CURRENT_TIMESTAMP WHERE %s=?1", s.Table, s.IDCol), id)
	return err
}

func (s *SQLiteGenericStore[T]) Exec(ctx context.Context, query string, params ...any) (int64, error) {
	r, err := s.Conn.ExecContext(ctx, query, params...)
	if err != nil { return 0, err }
	return r.RowsAffected()
}

func scanStruct(scanner interface{ Scan(dest ...any) error }, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be *struct")
	}
	ev := v.Elem()
	t := ev.Type()
	var ptrs []any
	ptrsToField := make(map[int]int)
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() { continue }
		fv := ev.Field(i)
		ft := fv.Type()
		if isJSONType(ft) {
			ptrsToField[len(ptrs)] = i
			ptrs = append(ptrs, reflect.New(reflect.TypeOf([]byte{})).Interface())
		} else {
			ptrs = append(ptrs, fv.Addr().Interface())
		}
	}
	if err := scanner.Scan(ptrs...); err != nil { return err }
	for pi, fi := range ptrsToField {
		b := ptrs[pi].(*[]byte)
		if b == nil || len(*b) == 0 { continue }
		json.Unmarshal(*b, ev.Field(fi).Addr().Interface())
	}
	return nil
}
