package storage

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

func (s *SQLiteGenericStore[T]) SqlScope(ctx context.Context) FlowgentSqlScope {
	return FlowgentSqlScopeForTable(ctx, s.Table)
}

func (s *SQLiteGenericStore[T]) Get(ctx context.Context, id string) (*T, error) {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	cols := utils.Columns[T]()
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	args := append([]any{id}, scopeArgs...)
	row := s.Conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s=?1 AND del_flag=0 AND (%s) LIMIT 1", cols, s.Table, s.IDCol, scopeWhere), args...)
	var entity T
	if err := utils.ScanStruct(row, &entity); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Table, err)
	}
	return &entity, nil
}

func (s *SQLiteGenericStore[T]) GetScoped(ctx context.Context, namespace, id string) (*T, error) {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	row := s.Conn.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT %s FROM %s WHERE namespace_id=?1 AND %s=?2 AND del_flag=0 AND (%s) LIMIT 1",
		utils.Columns[T](), s.Table, s.IDCol, scopeWhere), args...)
	entity := new(T)
	if err := utils.ScanStruct(row, entity); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Table, err)
	}
	return entity, nil
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
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	if err := s.Conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(1) FROM %s WHERE del_flag=0 AND (%s)", s.Table, scopeWhere), scopeArgs...).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size

	queryArgs := append(append([]any(nil), scopeArgs...), req.Size, offset)
	rows, err := s.Conn.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE del_flag=0 AND (%s) ORDER BY created_at DESC LIMIT ? OFFSET ?", cols, s.Table, scopeWhere), queryArgs...)
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

func (s *SQLiteGenericStore[T]) SelectScoped(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[T], error) {
	if err := utils.ValidateIdent(s.Table); err != nil {
		return nil, err
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	var total int64
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	countArgs := append([]any{namespace}, scopeArgs...)
	if err := s.Conn.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(1) FROM %s WHERE namespace_id=?1 AND del_flag=0 AND (%s)", s.Table, scopeWhere), countArgs...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(countArgs, req.Size, (req.Page-1)*req.Size)
	rows, err := s.Conn.QueryContext(ctx, fmt.Sprintf(
		"SELECT %s FROM %s WHERE namespace_id=?1 AND del_flag=0 AND (%s) ORDER BY created_at DESC LIMIT ? OFFSET ?",
		utils.Columns[T](), s.Table, scopeWhere), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*T, 0)
	for rows.Next() {
		entity := new(T)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		items = append(items, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	idIndex := -1
	holders := make([]string, len(cols))
	updates := make([]string, len(cols))
	for i, col := range cols {
		if col == s.IDCol {
			idIndex = i
		}
		holders[i] = "?"
		updates[i] = fmt.Sprintf("%s=excluded.%s", sqliteQuote(col), sqliteQuote(col))
	}
	if idIndex < 0 {
		return fmt.Errorf("%s: missing identity column %s", s.Table, s.IDCol)
	}
	scope := s.SqlScope(ctx)
	if scope.Where == "0=1" {
		return ErrFlowgentSqlScopeDenied
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		sqliteQuote(s.Table), sqliteQuoteCols(cols), strings.Join(holders, ","),
		sqliteQuote(s.IDCol), strings.Join(updates, ","))
	tx, err := s.Conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, sql, args...); err != nil {
		return err
	}
	if scope.Where != "1=1" {
		scopeWhere, scopeArgs := scope.SQLiteWhere()
		queryArgs := append([]any{args[idIndex]}, scopeArgs...)
		var visible int
		if err := tx.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT COUNT(1) FROM %s WHERE %s=? AND (%s)",
			sqliteQuote(s.Table), sqliteQuote(s.IDCol), scopeWhere), queryArgs...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit()
}

func sqliteQuote(identifier string) string { return `"` + identifier + `"` }

func sqliteQuoteCols(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = sqliteQuote(column)
	}
	return strings.Join(quoted, ",")
}

func (s *SQLiteGenericStore[T]) Delete(ctx context.Context, id string) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	args := append([]any{id}, scopeArgs...)
	_, err := s.Conn.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=1, status='DELETED', updated_at=CURRENT_TIMESTAMP WHERE %s=?1 AND (%s)", s.Table, s.IDCol, scopeWhere), args...)
	return err
}

func (s *SQLiteGenericStore[T]) DeleteScoped(ctx context.Context, namespace, id string) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).sqliteWhere()
	args := append([]any{namespace, id}, scopeArgs...)
	_, err := s.Conn.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET del_flag=1,status='DELETED',updated_at=CURRENT_TIMESTAMP WHERE namespace_id=?1 AND %s=?2 AND del_flag=0 AND (%s)",
		s.Table, s.IDCol, scopeWhere), args...)
	return err
}

func (s *SQLiteGenericStore[T]) Exec(ctx context.Context, query string, params ...any) (int64, error) {
	r, err := s.Conn.ExecContext(ctx, query, params...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
