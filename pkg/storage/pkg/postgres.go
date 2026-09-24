package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPostgresPool(ctx context.Context, dsn, schema string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		panic(fmt.Sprintf("pg: %v", err))
	}
	cfg.MaxConns, cfg.MinConns = 20, 2
	if schema == "" {
		schema = "public"
	}
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	searchPath := quotedSchema
	if schema != "public" {
		// PostgreSQL extensions are database-scoped and are conventionally
		// installed in public. Keep the tenant schema first for application
		// objects while retaining access to extension types and operators such
		// as pgvector's vector/halfvec.
		searchPath += ", public"
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+searchPath)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		panic(fmt.Sprintf("pg connect: %v", err))
	}
	if err := pool.Ping(ctx); err != nil {
		panic(fmt.Sprintf("pg ping: %v", err))
	}
	return pool
}

type PostgresGenericStore[T any] struct {
	Pool  *pgxpool.Pool
	Table string
	IDCol string
}

func (s *PostgresGenericStore[T]) SqlScope(ctx context.Context) FlowgentSqlScope {
	return FlowgentSqlScopeForTable(ctx, s.Table)
}

func (s *PostgresGenericStore[T]) Get(ctx context.Context, id string) (*T, error) {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	cols := utils.Columns[T]()
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE "%s"=$1 AND "status"<>'DELETED' AND (%s) LIMIT 1`, cols, s.Table, s.IDCol, scopeWhere), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("%s not found: %s=%s", s.Table, s.IDCol, id)
	}
	var entity T
	if err := utils.ScanStruct(rows, &entity); err != nil {
		return nil, err
	}
	return &entity, nil
}

func (s *PostgresGenericStore[T]) GetScoped(ctx context.Context, namespace, id string) (*T, error) {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	row := s.Pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE "namespace_id"=$1 AND "%s"=$2 AND "status"<>'DELETED' AND (%s) LIMIT 1`,
		utils.Columns[T](), s.Table, s.IDCol, scopeWhere), args...)
	entity := new(T)
	if err := utils.ScanStruct(row, entity); err != nil {
		return nil, fmt.Errorf("%s: %w", s.Table, err)
	}
	return entity, nil
}

func (s *PostgresGenericStore[T]) Select(ctx context.Context, req entities.PageRequest) (*entities.Page[T], error) {
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
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(1)
	if err := s.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(1) FROM %s WHERE "status"<>'DELETED' AND (%s)`, s.Table, scopeWhere), scopeArgs...).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size

	limitParameter := len(scopeArgs) + 1
	queryArgs := append(append([]any(nil), scopeArgs...), req.Size, offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE "status"<>'DELETED' AND (%s) ORDER BY "created_at" DESC LIMIT $%d OFFSET $%d`,
		cols, s.Table, scopeWhere, limitParameter, limitParameter+1), queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*T
	for rows.Next() {
		entity := new(T)
		if err := utils.ScanStruct(rows, entity); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		items = append(items, entity)
	}
	return entities.NewPage(items, total, req), nil
}

func (s *PostgresGenericStore[T]) SelectScoped(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[T], error) {
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
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(2)
	countArgs := append([]any{namespace}, scopeArgs...)
	if err := s.Pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(1) FROM %s WHERE "namespace_id"=$1 AND "status"<>'DELETED' AND (%s)`, s.Table, scopeWhere), countArgs...).Scan(&total); err != nil {
		return nil, err
	}
	limitParameter := len(countArgs) + 1
	queryArgs := append(countArgs, req.Size, (req.Page-1)*req.Size)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(
		`SELECT %s FROM %s WHERE "namespace_id"=$1 AND "status"<>'DELETED' AND (%s) ORDER BY "created_at" DESC LIMIT $%d OFFSET $%d`,
		utils.Columns[T](), s.Table, scopeWhere, limitParameter, limitParameter+1), queryArgs...)
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

func (s *PostgresGenericStore[T]) Save(ctx context.Context, entity *T) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	cols, args := utils.StructFields(entity)
	if len(cols) == 0 {
		return fmt.Errorf("no fields on %T", entity)
	}
	idIndex := -1
	holders := make([]string, len(cols))
	updates := make([]string, len(cols))
	for i, c := range cols {
		if c == s.IDCol {
			idIndex = i
		}
		holders[i] = fmt.Sprintf("$%d", i+1)
		updates[i] = fmt.Sprintf("%s=EXCLUDED.%s", pqQuote(c), pqQuote(c))
	}
	if idIndex < 0 {
		return fmt.Errorf("%s: missing identity column %s", s.Table, s.IDCol)
	}
	scope := s.SqlScope(ctx)
	if scope.Where == "0=1" {
		return ErrFlowgentSqlScopeDenied
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		s.Table, pqQuoteCols(cols), strings.Join(holders, ","), pqQuote(s.IDCol), strings.Join(updates, ","))
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return err
	}
	if scope.Where != "1=1" {
		scopeWhere, scopeArgs := scope.PostgresWhere(2)
		queryArgs := append([]any{args[idIndex]}, scopeArgs...)
		var visible int
		if err := tx.QueryRow(ctx, fmt.Sprintf(
			"SELECT COUNT(1) FROM %s WHERE %s=$1 AND (%s)",
			s.Table, pqQuote(s.IDCol), scopeWhere), queryArgs...).Scan(&visible); err != nil {
			return err
		}
		if visible != 1 {
			return ErrFlowgentSqlScopeDenied
		}
	}
	return tx.Commit(ctx)
}

func pqQuote(ident string) string {
	return `"` + ident + `"`
}

func pqQuoteCols(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pqQuote(c)
	}
	return strings.Join(quoted, ",")
}

func (s *PostgresGenericStore[T]) Delete(ctx context.Context, id string) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	_, err := s.Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE %s SET "status"='DELETED', "updated_at"=NOW() WHERE "%s"=$1 AND (%s)`, s.Table, s.IDCol, scopeWhere), args...)
	return err
}

func (s *PostgresGenericStore[T]) DeleteScoped(ctx context.Context, namespace, id string) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	scopeWhere, scopeArgs := s.SqlScope(ctx).postgresWhere(3)
	args := append([]any{namespace, id}, scopeArgs...)
	_, err := s.Pool.Exec(ctx, fmt.Sprintf(
		`UPDATE %s SET "status"='DELETED',"updated_at"=NOW()
		 WHERE "namespace_id"=$1 AND "%s"=$2 AND "status"<>'DELETED' AND (%s)`, s.Table, s.IDCol, scopeWhere), args...)
	return err
}

func (s *PostgresGenericStore[T]) Exec(ctx context.Context, sql string, params ...any) (int64, error) {
	tag, err := s.Pool.Exec(ctx, sql, params...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
