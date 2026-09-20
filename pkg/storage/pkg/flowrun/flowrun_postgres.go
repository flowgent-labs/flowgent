package flowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FlowRunPostgresStore wraps storage.PostgresGenericStore[entities.FlowRunInfo].
type FlowRunPostgresStore struct {
	inner *storage.PostgresGenericStore[entities.FlowRunInfo]
}

func NewFlowRunPostgresStore(pool *pgxpool.Pool) *FlowRunPostgresStore {
	return &FlowRunPostgresStore{
		inner: &storage.PostgresGenericStore[entities.FlowRunInfo]{
			Pool: pool, Table: "orh_flowrun", IDCol: "id",
		},
	}
}

func (s *FlowRunPostgresStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *FlowRunPostgresStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	if filter.Page.Page < 1 {
		filter.Page.Page = 1
	}
	if filter.Page.Size < 1 {
		filter.Page.Size = 50
	}
	clauses := []string{"del_flag=FALSE", "namespace_id=$1"}
	args := []any{filter.Namespace}
	add := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	add("status", filter.Status)
	add("runtime_mode", filter.RuntimeMode)
	add("namespace", filter.K8sNamespace)
	add("agentflow_id", filter.FlowID)
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(len(args) + 1)
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.inner.Pool.QueryRow(ctx, "SELECT COUNT(1) FROM orh_flowrun WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, filter.Page.Size, (filter.Page.Page-1)*filter.Page.Size)
	rows, err := s.inner.Pool.Query(ctx, fmt.Sprintf(
		"SELECT %s FROM orh_flowrun WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		utils.Columns[entities.FlowRunInfo](), where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowRunInfo, 0)
	for rows.Next() {
		item := new(entities.FlowRunInfo)
		if err := utils.ScanStruct(rows, item); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, filter.Page), nil
}

func (s *FlowRunPostgresStore) Metrics(ctx context.Context, req MetricRequest) (*entities.RunMetrics, error) {
	widthSeconds := req.Until.Sub(req.Since).Seconds() / float64(req.Buckets)
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(5)
	args := append([]any{req.Namespace, req.Since, req.Until, widthSeconds}, scopeArgs...)
	rows, err := s.inner.Pool.Query(ctx, `SELECT
		FLOOR(EXTRACT(EPOCH FROM (created_at - $2::timestamptz)) / $4)::int AS bucket,
		status, COUNT(1)
		FROM orh_flowrun
		WHERE del_flag=FALSE AND namespace_id=$1 AND created_at >= $2 AND created_at < $3 AND (`+scopeWhere+`)
		GROUP BY bucket, status ORDER BY bucket`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make([]metricRow, 0)
	for rows.Next() {
		var row metricRow
		if err := rows.Scan(&row.bucket, &row.status, &row.count); err != nil {
			return nil, err
		}
		counts = append(counts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildMetrics(req, counts), nil
}
func (s *FlowRunPostgresStore) HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error) {
	var active bool
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(3)
	args := append([]any{namespace, flowID}, scopeArgs...)
	err := s.inner.Pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM orh_flowrun
		WHERE namespace_id=$1 AND agentflow_id=$2 AND del_flag=FALSE
		  AND status IN ('PENDING','RUNNING','PAUSED') AND (`+scopeWhere+`)
	)`, args...).Scan(&active)
	return active, err
}
func (s *FlowRunPostgresStore) Save(ctx context.Context, e *entities.FlowRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowRunPostgresStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Create generates a UUID and sets timestamps before inserting.
func (s *FlowRunPostgresStore) Create(ctx context.Context, e *entities.FlowRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// Update performs a targeted update of mutable columns. Vars/Output are
// JSON-encoded explicitly (rather than relying on pgx's jsonb type
// inference) to mirror FlowRunSQLiteStore.Update and stay driver-agnostic.
func (s *FlowRunPostgresStore) Update(ctx context.Context, e *entities.FlowRunInfo) error {
	vars, err := json.Marshal(e.Vars)
	if err != nil {
		return err
	}
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(8)
	args := append([]any{e.Status, vars, output, e.Error, e.StartedAt, e.FinishedAt, e.ID}, scopeArgs...)
	_, err = s.inner.Pool.Exec(ctx,
		`UPDATE orh_flowrun SET status=$1, vars=$2, output=$3, error=$4, started_at=$5, finished_at=$6, updated_at=NOW() WHERE id=$7 AND (`+scopeWhere+`)`,
		args...)
	return err
}

// Cancel sets the run status to CANCELLED.
func (s *FlowRunPostgresStore) Cancel(ctx context.Context, id string) error {
	scopeWhere, scopeArgs := s.inner.SqlScope(ctx).PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	_, err := s.inner.Pool.Exec(ctx,
		`UPDATE orh_flowrun SET status='CANCELLED', updated_at=NOW() WHERE id=$1 AND (`+scopeWhere+`)`, args...)
	return err
}
