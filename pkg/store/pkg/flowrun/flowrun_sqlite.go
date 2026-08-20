package flowrun

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/google/uuid"
)

// FlowRunSQLiteStore wraps store.SQLiteGenericStore[entities.FlowRunInfo].
type FlowRunSQLiteStore struct {
	inner *store.SQLiteGenericStore[entities.FlowRunInfo]
}

func NewFlowRunSQLiteStore(conn *sql.DB) *FlowRunSQLiteStore {
	return &FlowRunSQLiteStore{
		inner: &store.SQLiteGenericStore[entities.FlowRunInfo]{
			Conn: conn, Table: "orh_flowrun", IDCol: "id",
		},
	}
}
func (s *FlowRunSQLiteStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	return s.inner.Get(ctx, id)
}
func (s *FlowRunSQLiteStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	if filter.Page.Page < 1 {
		filter.Page.Page = 1
	}
	if filter.Page.Size < 1 {
		filter.Page.Size = 50
	}
	clauses := []string{"del_flag=0", "namespace_id=?"}
	args := []any{filter.Namespace}
	add := func(column, value string) {
		if value == "" {
			return
		}
		clauses = append(clauses, column+"=?")
		args = append(args, value)
	}
	add("status", filter.Status)
	add("runtime_mode", filter.RuntimeMode)
	add("namespace", filter.K8sNamespace)
	add("agentflow_id", filter.FlowID)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.inner.Conn.QueryRowContext(ctx, "SELECT COUNT(1) FROM orh_flowrun WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), filter.Page.Size, (filter.Page.Page-1)*filter.Page.Size)
	rows, err := s.inner.Conn.QueryContext(ctx, fmt.Sprintf(
		"SELECT %s FROM orh_flowrun WHERE %s ORDER BY created_at DESC LIMIT ? OFFSET ?",
		utils.Columns[entities.FlowRunInfo](), where), queryArgs...)
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

func (s *FlowRunSQLiteStore) Metrics(ctx context.Context, req MetricRequest) (*entities.RunMetrics, error) {
	widthSeconds := req.Until.Sub(req.Since).Seconds() / float64(req.Buckets)
	rows, err := s.inner.Conn.QueryContext(ctx, `SELECT
		CAST(((julianday(created_at) - julianday(?2)) * 86400.0) / ?4 AS INTEGER) AS bucket,
		status, COUNT(1)
		FROM orh_flowrun
		WHERE del_flag=0 AND namespace_id=?1 AND created_at >= ?2 AND created_at < ?3
		GROUP BY bucket, status ORDER BY bucket`, req.Namespace, req.Since, req.Until, widthSeconds)
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
func (s *FlowRunSQLiteStore) HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error) {
	var active bool
	err := s.inner.Conn.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM orh_flowrun
		WHERE namespace_id=?1 AND agentflow_id=?2 AND del_flag=0
		  AND status IN ('PENDING','RUNNING','PAUSED')
	)`, namespace, flowID).Scan(&active)
	return active, err
}
func (s *FlowRunSQLiteStore) Save(ctx context.Context, e *entities.FlowRunInfo) error {
	return s.inner.Save(ctx, e)
}
func (s *FlowRunSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.inner.Delete(ctx, id)
}

// Create generates a UUID and sets timestamps before inserting.
func (s *FlowRunSQLiteStore) Create(ctx context.Context, e *entities.FlowRunInfo) error {
	e.ID = uuid.New().String()
	now := time.Now().UTC()
	e.CreatedAt = now
	e.UpdatedAt = now
	return s.inner.Save(ctx, e)
}

// Update performs a targeted update of mutable columns. Vars/Output are
// JSON-encoded before binding — database/sql (unlike pgx) has no built-in
// support for map[string]any parameters.
func (s *FlowRunSQLiteStore) Update(ctx context.Context, e *entities.FlowRunInfo) error {
	vars, err := json.Marshal(e.Vars)
	if err != nil {
		return err
	}
	output, err := json.Marshal(e.Output)
	if err != nil {
		return err
	}
	_, err = s.inner.Conn.ExecContext(ctx,
		`UPDATE orh_flowrun SET status=?1, vars=?2, output=?3, error=?4, started_at=?5, finished_at=?6, updated_at=CURRENT_TIMESTAMP WHERE id=?7`,
		string(e.Status), vars, output, e.Error, e.StartedAt, e.FinishedAt, e.ID)
	return err
}

// Cancel sets the run status to CANCELLED.
func (s *FlowRunSQLiteStore) Cancel(ctx context.Context, id string) error {
	_, err := s.inner.Conn.ExecContext(ctx,
		`UPDATE orh_flowrun SET status='CANCELLED', updated_at=CURRENT_TIMESTAMP WHERE id=?1`, id)
	return err
}
