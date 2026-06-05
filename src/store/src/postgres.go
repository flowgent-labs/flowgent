package store

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/common/src/tracing"
	"github.com/flowgent-labs/flowgent/model/src"
)

var pgTracer = tracing.Tracer("flowgent/postgres")

// NewPostgresPool creates a pgxpool from DSN.
func NewPostgresPool(ctx context.Context, dsn, schema string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil { panic(fmt.Sprintf("pg config: %v", err)) }
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

// pgStore implements IStore backed by a shared pgxpool.
type pgStore struct{ PostgresStore }

func newPgStore(pool *pgxpool.Pool) *pgStore {
	return &pgStore{PostgresStore: PostgresStore{Pool: pool}}
}

type PostgresStore struct {
	Pool   *pgxpool.Pool
	DSN    string
	Schema string
}

func NewPostgresStore(dsn string) *PostgresStore {
	return &PostgresStore{DSN: dsn}
}

func (s *PostgresStore) DB() any  { return s.Pool }
func (s *PostgresStore) SetPoolConfig(_, _ int) {}
func (s *PostgresStore) SetSchema(schema string) { s.Schema = schema }

func (s *PostgresStore) Init(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(s.DSN)
	if err != nil { return fmt.Errorf("parse pg config: %w", err) }
	cfg.MaxConns, cfg.MinConns = 20, 2
	schema := s.Schema
	if schema == "" { schema = "public" }
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET search_path TO %s", schema))
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil { return fmt.Errorf("connect postgres: %w", err) }
	if err := pool.Ping(ctx); err != nil { return fmt.Errorf("ping postgres: %w", err) }
	s.Pool = pool
	return nil
}

func (s *PostgresStore) Close() error {
	if s.Pool != nil { s.Pool.Close() }
	return nil
}

// ─── Row helpers ──────────────────────────────────────────

type pgxRows interface {
	Close()
	Next() bool
	Scan(...any) error
	Err() error
}

func collectRunRows(rows pgxRows) ([]model.AgentFlowRun, error) {
	var runs []model.AgentFlowRun
	for rows.Next() {
		r, err := scanAgentFlowRunRow(rows)
		if err != nil {
			log.Printf("[pg] scanAgentFlowRun error: %v", err)
			continue
		}
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

// ═══════════════════════════════════════════════════════════════
// Generic Entity Store — PostgresStore[T] (Rust PostgresRepository<T> pattern)
// ═══════════════════════════════════════════════════════════════

// EntityStore is a typed generic store for one entity table.
// Entity-specific stores wrap this with their concrete type and add custom queries.
type EntityStore[T any] struct {
	Pool  *pgxpool.Pool
	Table string
	IDCol string
}

func (s *EntityStore[T]) Get(ctx context.Context, id string) (*T, error) {
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE %s=$1 LIMIT 1", s.Table, s.IDCol), id)
	if err != nil { return nil, err }
	defer rows.Close()
	return pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[T])
}

func (s *EntityStore[T]) Select(ctx context.Context, offset, limit int) ([]*T, error) {
	rows, err := s.Pool.Query(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE del_flag=false ORDER BY created_at DESC LIMIT $1 OFFSET $2", s.Table), limit, offset)
	if err != nil { return nil, err }
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[T])
}

func (s *EntityStore[T]) Save(ctx context.Context, entity *T) error {
	cols, args := structFields(entity)
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

func (s *EntityStore[T]) Delete(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET del_flag=true, status='DELETED', updated_at=$1 WHERE %s=$2", s.Table, s.IDCol),
		time.Now(), id)
	return err
}

func (s *EntityStore[T]) Execute(ctx context.Context, sql string, params ...any) ([]*T, error) {
	rows, err := s.Pool.Query(ctx, sql, params...)
	if err != nil { return nil, err }
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[T])
}

func (s *EntityStore[T]) Exec(ctx context.Context, sql string, params ...any) (int64, error) {
	tag, err := s.Pool.Exec(ctx, sql, params...)
	if err != nil { return 0, err }
	return tag.RowsAffected(), nil
}

// ─── Reflection ──────────────────────────────────────────

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

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' { b.WriteByte('_') }
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
