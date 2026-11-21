package graphrun

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

type GraphRunPostgresStore struct{ pool *pgxpool.Pool }

func NewGraphRunPostgresStore(pool *pgxpool.Pool) *GraphRunPostgresStore {
	return &GraphRunPostgresStore{pool: pool}
}

func (s *GraphRunPostgresStore) Get(ctx context.Context, id string) (*entities.GraphRun, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(2)
	args := append([]any{id}, scopeArgs...)
	var record runRecord
	query := `SELECT ` + utils.Columns[runRecord]() + ` FROM (
		SELECT r.*,f.name AS agent_graph_name FROM (
			SELECT * FROM orh_graph_run WHERE id=$1 AND status<>'DELETED' AND (` + scopeWhere + `)
		) r JOIN orh_agent_graph f ON f.id=r.agent_graph_id
	) current_run`
	if err := utils.ScanStruct(s.pool.QueryRow(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}

func (s *GraphRunPostgresStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.GraphRun], error) {
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
	if filter.Status == "PENDING,RUNNING" {
		clauses = append(clauses, "status IN ('PENDING','RUNNING')")
	} else {
		add("status", filter.Status)
	}
	add("runtime_mode", filter.RuntimeMode)
	add("runtime_cluster_id", filter.K8sNamespace)
	if filter.AgentGraphID != "" {
		args = append(args, filter.AgentGraphID)
		clauses = append(clauses, fmt.Sprintf("agent_graph_id IN (SELECT id FROM orh_agent_graph WHERE namespace_id=$1 AND (id=$%d OR name=$%d))", len(args), len(args)))
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(len(args) + 1)
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM orh_graph_run WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, filter.Page.Size, (filter.Page.Page-1)*filter.Page.Size)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM (
		SELECT r.*,f.name AS agent_graph_name FROM (SELECT * FROM orh_graph_run WHERE %s) r
		JOIN orh_agent_graph f ON f.id=r.agent_graph_id
	) current_run ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[runRecord](), where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.GraphRun, 0)
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

func (s *GraphRunPostgresStore) Metrics(ctx context.Context, req MetricRequest) (*entities.GraphRunMetrics, error) {
	if req.Buckets < 1 || !req.Until.After(req.Since) {
		return buildMetrics(req, nil), nil
	}
	width := req.Until.Sub(req.Since).Seconds() / float64(req.Buckets)
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(5)
	args := append([]any{req.Namespace, req.Since, req.Until, width}, scopeArgs...)
	rows, err := s.pool.Query(ctx, `SELECT FLOOR(EXTRACT(EPOCH FROM(created_at-$2::timestamptz))/$4)::int,status,COUNT(*),COALESCE(SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN EXTRACT(EPOCH FROM(finished_at-started_at))*1000 ELSE 0 END),0)::bigint,SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN 1 ELSE 0 END) FROM orh_graph_run WHERE status<>'DELETED' AND namespace_id=$1 AND created_at>=$2 AND created_at<$3 AND (`+scopeWhere+`) GROUP BY 1,status ORDER BY 1`, args...)
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

func (s *GraphRunPostgresStore) HasActiveForAgentGraph(ctx context.Context, namespace, graphID string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(3)
	args := append([]any{namespace, graphID}, scopeArgs...)
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orh_graph_run WHERE namespace_id=$1
		AND agent_graph_id IN (SELECT id FROM orh_agent_graph WHERE namespace_id=$1 AND (id=$2 OR name=$2))
		AND status IN('PENDING','RUNNING','PAUSED') AND (`+scopeWhere+`))`, args...).Scan(&active)
	return active, err
}
func (s *GraphRunPostgresStore) Save(ctx context.Context, item *entities.GraphRun) error {
	if item.ID == "" {
		return s.Create(ctx, item)
	}
	return s.Update(ctx, item)
}

func (s *GraphRunPostgresStore) Create(ctx context.Context, item *entities.GraphRun) error {
	item.Normalize()
	if item.Namespace == "" {
		item.Namespace = "default"
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Status == "" {
		item.Status = entities.GraphRunPending
	}
	if item.RuntimeMode == "" {
		item.RuntimeMode = entities.RuntimeModeApplication
	}
	// Runtime materializations are never accepted from an authoring request.
	metadata := map[string]any{}
	for key, value := range item.Metadata {
		if key != "subgraphs" && key != "execution_progress" {
			metadata[key] = value
		}
	}
	item.Metadata = metadata
	principal := item.CreatedBy
	if principal == "" {
		principal = "system:flowgent"
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	graphKey := item.AgentGraphName
	if graphKey == "" {
		graphKey = item.AgentGraphID
	}
	var graphID, graphName, revisionID, checksum string
	var revision int64
	var defaultSummarize bool
	var definition []byte
	var graphInstruction, namespaceInstruction *string
	query := `SELECT f.id,f.name,r.id,r.revision,r.summarize_enabled,r.checksum,r.definition,f.instruction_id,n.instruction_id
		FROM orh_agent_graph f JOIN orh_agent_graph_revision r ON r.id=CASE WHEN $3='' THEN f.current_revision_id ELSE $3 END
		JOIN orh_namespace n ON n.id=f.namespace_id
		WHERE f.namespace_id=$1 AND (f.id=$2 OR f.name=$2) AND r.agent_graph_id=f.id AND f.status<>'DELETED' FOR SHARE OF f,r`
	if err = tx.QueryRow(ctx, query, item.Namespace, graphKey, item.AgentGraphRevisionID).Scan(&graphID, &graphName, &revisionID, &revision, &defaultSummarize, &checksum, &definition, &graphInstruction, &namespaceInstruction); err != nil {
		return fmt.Errorf("lock graph revision: %w", err)
	}
	snapshot := map[string]any{"system_instruction": map[string]any{"reference": "deployment://system-instruction"}, "agent_graph_revision": map[string]any{"agent_graph_id": graphID, "agent_graph_name": graphName, "revision_id": revisionID, "revision": revision, "checksum": checksum}, "instruction_revisions": map[string]any{"namespace": namespaceInstruction, "graph": graphInstruction}, "agent_revisions": map[string]any{}, "knowledge_revision_ids": []string{}}
	var spec entities.AgentGraph
	_ = json.Unmarshal(definition, &spec)
	spec.Revision = revision
	if err = pinSubGraphs(ctx, storage.WrapSQLTransaction(tx), item.Namespace, &spec, map[string]bool{}, 0); err != nil {
		return err
	}
	definition, err = json.Marshal(spec)
	if err != nil {
		return err
	}

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
	knowledgeArgs := append([]any{item.Namespace, graphID}, knwArgs...)
	rows, err := tx.Query(ctx, `SELECT current_revision_id FROM knw_document WHERE namespace_id=$1 AND current_revision_id IS NOT NULL AND status<>'DELETED' AND (scope='namespace' OR(scope='graph' AND agent_graph_id=$2)) AND (`+knwScope+`)`, knowledgeArgs...)
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
	_, err = tx.Exec(ctx, `INSERT INTO orh_graph_run(id,namespace_id,agent_graph_id,agent_graph_revision_id,status,input,output,error,run_instruction,summarize_enabled,context_snapshot,trigger_type,trigger_source,trigger_payload,runtime_mode,runtime_cluster_id,started_at,finished_at,description,created_by,updated_by,metadata,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$20,$21,$22)`, item.ID, item.Namespace, graphID, revisionID, item.Status, runJSON(item.Input), runJSON(item.Output), runError(item.Error), item.RunInstruction, effective, runJSON(snapshot), item.TriggerType, item.TriggerSource, runJSON(item.TriggerPayload), item.RuntimeMode, item.RuntimeClusterID, item.StartedAt, item.FinishedAt, item.Description, principal, runJSON(item.Metadata), definition)
	if err != nil {
		return err
	}
	if err = appendGraphRunCreated(ctx, storage.WrapSQLTransaction(tx), item, graphID, graphName); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_ = json.Unmarshal(definition, &item.Snapshot)
	item.AgentGraphRevisionID = revisionID
	item.AgentGraphID = graphID
	item.AgentGraphName = graphName
	item.AgentGraphRevision = revision
	item.SummarizeEnabled = effective
	item.ContextSnapshot = snapshot
	item.CreatedAt = now
	item.UpdatedAt = now
	item.RowVersion = 1
	return nil
}

func (s *GraphRunPostgresStore) Update(ctx context.Context, item *entities.GraphRun) error {
	item.Normalize()
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(8)
	args := append([]any{item.Status, runJSON(item.Input), runJSON(item.Output), runError(item.Error), item.StartedAt, item.FinishedAt, item.ID}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_graph_run SET status=$1,input=COALESCE($2,input),output=$3,error=$4,started_at=$5,finished_at=$6 WHERE id=$7 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
func (s *GraphRunPostgresStore) Delete(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, "DELETED")
}
func (s *GraphRunPostgresStore) Cancel(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, string(entities.GraphRunCancelled))
}
func (s *GraphRunPostgresStore) setStatus(ctx context.Context, id, status string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_graph_run").PostgresWhere(3)
	args := append([]any{status, id}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE orh_graph_run SET status=$1 WHERE id=$2 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
