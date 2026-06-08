package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg"
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
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
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

func (s *PostgresGenericStore[T]) Get(ctx context.Context, id string) (*T, error) {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return nil, err
	}
	cols := utils.Columns[T]()
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s=$1 LIMIT 1", cols, s.Table, s.IDCol), id)
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

func (s *PostgresGenericStore[T]) Select(ctx context.Context, req model.PageRequest) (*model.Page[T], error) {
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
	if err := s.Pool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(1) FROM %s", s.Table)).Scan(&total); err != nil {
		return nil, err
	}
	offset := (req.Page - 1) * req.Size

	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM %s ORDER BY created_at DESC LIMIT $1 OFFSET $2", cols, s.Table), req.Size, offset)
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
	return model.NewPage(items, total, req), nil
}

func (s *PostgresGenericStore[T]) Save(ctx context.Context, entity *T) error {
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	cols, args := utils.StructFields(entity)
	if len(cols) == 0 {
		return fmt.Errorf("no fields on %T", entity)
	}
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
	if err := utils.ValidateIdent(s.Table, s.IDCol); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=true, status='DELETED', updated_at=NOW() WHERE %s=$1", s.Table, s.IDCol), id)
	return err
}

func (s *PostgresGenericStore[T]) Exec(ctx context.Context, sql string, params ...any) (int64, error) {
	tag, err := s.Pool.Exec(ctx, sql, params...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
