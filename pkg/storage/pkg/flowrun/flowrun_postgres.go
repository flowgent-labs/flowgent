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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowRunPostgresStore struct{ pool *pgxpool.Pool }

func NewFlowRunPostgresStore(pool *pgxpool.Pool) *FlowRunPostgresStore {
	return &FlowRunPostgresStore{pool: pool}
}

func (s *FlowRunPostgresStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	var record runRecord
	query := `SELECT ` + utils.Columns[runRecord]() + ` FROM (
		SELECT r.*,f.name AS flow_name FROM (
			SELECT * FROM orh_run WHERE id=$1 AND status<>'DELETED' AND (` + scopeWhere + `)
		) r JOIN orh_flow f ON f.id=r.flow_id
	) current_run`
	if err := utils.ScanStruct(s.pool.QueryRow(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}

func (s *FlowRunPostgresStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	if filter.Page.Page < 1 {
		filter.Page.Page = 1
	}
	if filter.Page.Size < 1 {
		filter.Page.Size = 50
	}
	clauses := []string{"status<>'DELETED'", "namespace_id=$1"}
	args := []any{filter.Namespace}
	add := func(column, value string) {
		if value != "" {
			args = append(args, value)
			clauses = append(clauses, fmt.Sprintf("%s=$%d", column, len(args)))
		}
	}
	add("status", filter.Status)
	add("runtime_mode", filter.RuntimeMode)
	add("runtime_cluster_id", filter.K8sNamespace)
	if filter.FlowID != "" {
		args = append(args, filter.FlowID)
		clauses = append(clauses, fmt.Sprintf("flow_id IN (SELECT id FROM orh_flow WHERE namespace_id=$1 AND (id=$%d OR name=$%d))", len(args), len(args)))
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(len(args) + 1)
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM orh_run WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, filter.Page.Size, (filter.Page.Page-1)*filter.Page.Size)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM (
		SELECT r.*,f.name AS flow_name FROM (SELECT * FROM orh_run WHERE %s) r
		JOIN orh_flow f ON f.id=r.flow_id
	) current_run ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[runRecord](), where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.FlowRunInfo, 0)
	for rows.Next() {
		var record runRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			return nil, err
		}
		items = append(items, record.entity())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, filter.Page), nil
}

func (s *FlowRunPostgresStore) Metrics(ctx context.Context, req MetricRequest) (*entities.RunMetrics, error) {
	if req.Buckets < 1 || !req.Until.After(req.Since) {
		return buildMetrics(req, nil), nil
	}
	width := req.Until.Sub(req.Since).Seconds() / float64(req.Buckets)
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(5)
	args := append([]any{req.Namespace, req.Since, req.Until, width}, scopeArgs...)
	rows, err := s.pool.Query(ctx, `SELECT FLOOR(EXTRACT(EPOCH FROM(created_at-$2::timestamptz))/$4)::int,status,COUNT(*),COALESCE(SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN EXTRACT(EPOCH FROM(finished_at-started_at))*1000 ELSE 0 END),0)::bigint,SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN 1 ELSE 0 END) FROM orh_run WHERE status<>'DELETED' AND namespace_id=$1 AND created_at>=$2 AND created_at<$3 AND (`+scopeWhere+`) GROUP BY 1,status ORDER BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]metricRow, 0)
	for rows.Next() {
		var row metricRow
		if err := rows.Scan(&row.bucket, &row.status, &row.count, &row.durationTotalMs, &row.durationCount); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return buildMetrics(req, values), rows.Err()
}

func (s *FlowRunPostgresStore) HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(3)
	args := append([]any{namespace, flowID}, scopeArgs...)
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orh_run WHERE namespace_id=$1
		AND flow_id IN (SELECT id FROM orh_flow WHERE namespace_id=$1 AND (id=$2 OR name=$2))
		AND status IN('PENDING','RUNNING','PAUSED') AND (`+scopeWhere+`))`, args...).Scan(&active)
	return active, err
}
func (s *FlowRunPostgresStore) Save(ctx context.Context, item *entities.FlowRunInfo) error {
	if item.ID == "" {
		return s.Create(ctx, item)
	}
	return s.Update(ctx, item)
}

func (s *FlowRunPostgresStore) Create(ctx context.Context, item *entities.FlowRunInfo) error {
	item.NormalizeAliases()
	if item.Namespace == "" {
		item.Namespace = "default"
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Status == "" {
		item.Status = entities.RunPending
	}
	if item.RuntimeMode == "" {
		item.RuntimeMode = entities.RuntimeModeApplication
	}
	principal := item.CreatedBy
	if principal == "" {
		principal = "system:flowgent"
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	flowKey := item.FlowName
	if flowKey == "" {
		flowKey = item.FlowID
	}
	var flowID, flowName, revisionID, checksum string
	var revision int64
	var defaultSummarize bool
	var definition []byte
	var flowInstruction, namespaceInstruction *string
	query := `SELECT f.id,f.name,r.id,r.revision,r.summarize_enabled,r.checksum,r.definition,f.instruction_id,n.instruction_id
		FROM orh_flow f JOIN orh_flow_revision r ON r.id=CASE WHEN $3='' THEN f.current_revision_id ELSE $3 END
		JOIN orh_namespace n ON n.id=f.namespace_id
		WHERE f.namespace_id=$1 AND (f.id=$2 OR f.name=$2) AND r.flow_id=f.id AND f.status<>'DELETED' FOR SHARE OF f,r`
	if err = tx.QueryRow(ctx, query, item.Namespace, flowKey, item.FlowRevisionID).Scan(&flowID, &flowName, &revisionID, &revision, &defaultSummarize, &checksum, &definition, &flowInstruction, &namespaceInstruction); err != nil {
		return fmt.Errorf("lock flow revision: %w", err)
	}
	snapshot := map[string]any{"system_instruction": map[string]any{"reference": "deployment://system-instruction"}, "flow_revision": map[string]any{"flow_id": flowID, "flow_name": flowName, "revision_id": revisionID, "revision": revision, "checksum": checksum}, "instruction_revisions": map[string]any{"namespace": namespaceInstruction, "flow": flowInstruction}, "agent_revisions": map[string]any{}, "knowledge_revision_ids": []string{}}
	var spec entities.FlowInfo
	_ = json.Unmarshal(definition, &spec)
	agents := map[string]struct{}{}
	collectAgentNames(spec.Nodes, agents)
	lockedAgents := snapshot["agent_revisions"].(map[string]any)
	for name := range agents {
		var id string
		var rev int64
		if scanErr := tx.QueryRow(ctx, `SELECT r.id,r.revision FROM llm_agent a JOIN llm_agent_revision r ON r.id=a.current_revision_id WHERE a.namespace_id=$1 AND a.name=$2 AND a.status<>'DELETED'`, item.Namespace, name).Scan(&id, &rev); scanErr != nil {
			return fmt.Errorf("lock agent %s: %w", name, scanErr)
		}
		lockedAgents[name] = map[string]any{"revision_id": id, "revision": rev}
	}
	knwScope, knwArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").PostgresWhere(3)
	knowledgeArgs := append([]any{item.Namespace, flowID}, knwArgs...)
	rows, err := tx.Query(ctx, `SELECT current_revision_id FROM knw_document WHERE namespace_id=$1 AND current_revision_id IS NOT NULL AND status<>'DELETED' AND (scope='namespace' OR(scope='flow' AND flow_id=$2)) AND (`+knwScope+`)`, knowledgeArgs...)
	if err != nil {
		return err
	}
	knowledge := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		knowledge = append(knowledge, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	snapshot["knowledge_revision_ids"] = knowledge
	if item.ContextSnapshot != nil {
		if system, ok := item.ContextSnapshot["system_instruction"]; ok {
			snapshot["system_instruction"] = system
		}
	}
	effective := defaultSummarize
	if item.SummarizeOverride != nil {
		effective = *item.SummarizeOverride
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO orh_run(id,namespace_id,flow_id,flow_revision_id,status,input,output,error,run_instruction,summarize_enabled,context_snapshot,trigger_type,trigger_source,trigger_payload,runtime_mode,runtime_cluster_id,started_at,finished_at,description,created_by,updated_by,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20,$21)`, item.ID, item.Namespace, flowID, revisionID, item.Status, runJSON(item.Input), runJSON(item.Output), runError(item.Error), item.RunInstruction, effective, runJSON(snapshot), item.TriggerType, item.TriggerSource, runJSON(item.TriggerPayload), item.RuntimeMode, item.RuntimeClusterID, item.StartedAt, item.FinishedAt, item.Description, principal, runJSON(item.Metadata))
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	item.FlowRevisionID = revisionID
	item.FlowID = flowID
	item.FlowName = flowName
	item.AgentFlowID = flowName
	item.FlowRevision = revision
	item.Version = revision
	item.SummarizeEnabled = effective
	item.ContextSnapshot = snapshot
	item.CreatedAt = now
	item.UpdatedAt = now
	item.RowVersion = 1
	return nil
}

func (s *FlowRunPostgresStore) Update(ctx context.Context, item *entities.FlowRunInfo) error {
	item.NormalizeAliases()
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(8)
	args := append([]any{item.Status, runJSON(item.Input), runJSON(item.Output), runError(item.Error), item.StartedAt, item.FinishedAt, item.ID}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_run SET status=$1,input=COALESCE($2,input),output=$3,error=$4,started_at=$5,finished_at=$6 WHERE id=$7 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
func (s *FlowRunPostgresStore) Delete(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, "DELETED")
}
func (s *FlowRunPostgresStore) Cancel(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, string(entities.RunCancelled))
}
func (s *FlowRunPostgresStore) setStatus(ctx context.Context, id, status string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").PostgresWhere(3)
	args := append([]any{status, id}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_run SET status=$1 WHERE id=$2 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
