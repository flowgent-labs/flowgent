package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func NewSQLiteConn(ctx context.Context, dir string) *sql.DB {
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Sprintf("sqlite dir: %v", err))
	}
	dbPath := filepath.Join(dir, "flowgent.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		panic(fmt.Sprintf("sqlite open: %v", err))
	}
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
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	cols := utils.Columns[T]()
	row := s.Conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s=?1 LIMIT 1", cols, s.Table, s.IDCol), id)
	var entity T
	if err := utils.ScanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Table, err)
	}
	return &entity, nil
}

func (s *SQLiteGenericStore[T]) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[T], error) {
	if err := utils.ValidateIdent(s.Table); err != nil {
		return nil, err
	}
	cols := utils.Columns[T]()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}

	var total int64
	if err := s.Conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(1) FROM %s", s.Table)).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size

	rows, err := s.Conn.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s ORDER BY created_at DESC LIMIT ?1 OFFSET ?2", cols, s.Table), req.Size, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*T
	for rows.Next() {
		e := new(T)
		if err := utils.ScanStruct(rows, e); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		items = append(items, e)
	}
	return entities.NewPage(items, total, req), nil
}

func (s *SQLiteGenericStore[T]) Save(ctx context.Context, entity *T) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	cols, args := utils.StructFields(entity)
	if len(cols) == 0 {
		return fmt.Errorf("no fields")
	}
	holders := make([]string, len(cols))
	for i := range cols {
		holders[i] = "?"
	}
	sql := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		s.Table, strings.Join(cols, ","), strings.Join(holders, ","))
	_, err := s.Conn.ExecContext(ctx, sql, args...)
	return err
}

func (s *SQLiteGenericStore[T]) Delete(ctx context.Context, id string) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	_, err := s.Conn.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=1, status='DELETED', updated_at=CURRENT_TIMESTAMP WHERE %s=?1", s.Table, s.IDCol), id)
	return err
}

func (s *SQLiteGenericStore[T]) Exec(ctx context.Context, query string, params ...any) (int64, error) {
	r, err := s.Conn.ExecContext(ctx, query, params...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
