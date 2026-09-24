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
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type FlowRunSQLiteStore struct{ conn *sql.DB }

func NewFlowRunSQLiteStore(conn *sql.DB) *FlowRunSQLiteStore { return &FlowRunSQLiteStore{conn: conn} }
func (s *FlowRunSQLiteStore) Get(ctx context.Context, id string) (*entities.FlowRunInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	args := append([]any{id}, scopeArgs...)
	var record runRecord
	query := `SELECT ` + utils.Columns[runRecord]() + ` FROM (
		SELECT r.*,f.name AS flow_name FROM (
			SELECT * FROM orh_run WHERE id=?1 AND status<>'DELETED' AND (` + scopeWhere + `)
		) r JOIN orh_flow f ON f.id=r.flow_id
	) current_run`
	if err := utils.ScanStruct(s.conn.QueryRowContext(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}
func (s *FlowRunSQLiteStore) List(ctx context.Context, filter ListFilter) (*entities.Page[entities.FlowRunInfo], error) {
	if filter.Page.Page < 1 {
		filter.Page.Page = 1
	}
	if filter.Page.Size < 1 {
		filter.Page.Size = 50
	}
	clauses := []string{"status<>'DELETED'", "namespace_id=?"}
	args := []any{filter.Namespace}
	add := func(column, value string) {
		if value != "" {
			clauses = append(clauses, column+"=?")
			args = append(args, value)
		}
	}
	add("status", filter.Status)
	add("runtime_mode", filter.RuntimeMode)
	add("runtime_cluster_id", filter.K8sNamespace)
	if filter.FlowID != "" {
		clauses = append(clauses, "flow_id IN (SELECT id FROM orh_flow WHERE namespace_id=? AND (id=? OR name=?))")
		args = append(args, filter.Namespace, filter.FlowID, filter.FlowID)
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	clauses = append(clauses, "("+scopeWhere+")")
	args = append(args, scopeArgs...)
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := s.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM orh_run WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	queryArgs := append(append([]any{}, args...), filter.Page.Size, (filter.Page.Page-1)*filter.Page.Size)
	rows, err := s.conn.QueryContext(ctx, `SELECT `+utils.Columns[runRecord]()+` FROM (
		SELECT r.*,f.name AS flow_name FROM (SELECT * FROM orh_run WHERE `+where+`) r
		JOIN orh_flow f ON f.id=r.flow_id
	) current_run ORDER BY created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
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
func (s *FlowRunSQLiteStore) Metrics(ctx context.Context, req MetricRequest) (*entities.RunMetrics, error) {
	if req.Buckets < 1 || !req.Until.After(req.Since) {
		return buildMetrics(req, nil), nil
	}
	width := req.Until.Sub(req.Since).Seconds() / float64(req.Buckets)
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	args := append([]any{req.Namespace, req.Since, req.Until, width}, scopeArgs...)
	rows, err := s.conn.QueryContext(ctx, `SELECT CAST(((julianday(created_at)-julianday(?2))*86400.0)/?4 AS INTEGER),status,COUNT(*),COALESCE(SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN CAST((julianday(finished_at)-julianday(started_at))*86400000 AS INTEGER) ELSE 0 END),0),SUM(CASE WHEN started_at IS NOT NULL AND finished_at IS NOT NULL THEN 1 ELSE 0 END) FROM orh_run WHERE status<>'DELETED' AND namespace_id=?1 AND created_at>=?2 AND created_at<?3 AND (`+scopeWhere+`) GROUP BY 1,status ORDER BY 1`, args...)
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
func (s *FlowRunSQLiteStore) HasActiveForFlow(ctx context.Context, namespace, flowID string) (bool, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	args := append([]any{namespace, flowID}, scopeArgs...)
	var active bool
	err := s.conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orh_run WHERE namespace_id=?1
		AND flow_id IN (SELECT id FROM orh_flow WHERE namespace_id=?1 AND (id=?2 OR name=?2))
		AND status IN('PENDING','RUNNING','PAUSED') AND (`+scopeWhere+`))`, args...).Scan(&active)
	return active, err
}
func (s *FlowRunSQLiteStore) Save(ctx context.Context, item *entities.FlowRunInfo) error {
	if item.ID == "" {
		return s.Create(ctx, item)
	}
	return s.Update(ctx, item)
}
func (s *FlowRunSQLiteStore) Create(ctx context.Context, item *entities.FlowRunInfo) error {
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
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	flowKey := item.FlowName
	if flowKey == "" {
		flowKey = item.FlowID
	}
	var flowID, flowName, revisionID, checksum string
	var revision int64
	var defaultSummarize bool
	var definition []byte
	var flowInstruction, namespaceInstruction sql.NullString
	query := `SELECT f.id,f.name,r.id,r.revision,r.summarize_enabled,r.checksum,r.definition,f.instruction_id,n.instruction_id
		FROM orh_flow f JOIN orh_flow_revision r ON r.id=CASE WHEN ?3='' THEN f.current_revision_id ELSE ?3 END
		JOIN orh_namespace n ON n.id=f.namespace_id
		WHERE f.namespace_id=?1 AND (f.id=?2 OR f.name=?2) AND r.flow_id=f.id AND f.status<>'DELETED'`
	if err = tx.QueryRowContext(ctx, query, item.Namespace, flowKey, item.FlowRevisionID).Scan(&flowID, &flowName, &revisionID, &revision, &defaultSummarize, &checksum, &definition, &flowInstruction, &namespaceInstruction); err != nil {
		return fmt.Errorf("lock flow revision: %w", err)
	}
	snapshot := map[string]any{"system_instruction": map[string]any{"reference": "deployment://system-instruction"}, "flow_revision": map[string]any{"flow_id": flowID, "flow_name": flowName, "revision_id": revisionID, "revision": revision, "checksum": checksum}, "instruction_revisions": map[string]any{"namespace": nullableString(namespaceInstruction), "flow": nullableString(flowInstruction)}, "agent_revisions": map[string]any{}, "knowledge_revision_ids": []string{}}
	var spec entities.FlowInfo
	_ = json.Unmarshal(definition, &spec)
	agents := map[string]struct{}{}
	collectAgentNames(spec.Nodes, agents)
	lockedAgents := snapshot["agent_revisions"].(map[string]any)
	for name := range agents {
		var id string
		var rev int64
		if scanErr := tx.QueryRowContext(ctx, `SELECT r.id,r.revision FROM llm_agent a JOIN llm_agent_revision r ON r.id=a.current_revision_id WHERE a.namespace_id=? AND a.name=? AND a.status<>'DELETED'`, item.Namespace, name).Scan(&id, &rev); scanErr != nil {
			return fmt.Errorf("lock agent %s: %w", name, scanErr)
		}
		lockedAgents[name] = map[string]any{"revision_id": id, "revision": rev}
	}
	knwScope, knwArgs := storage.FlowgentSqlScopeForTable(ctx, "knw_document").SQLiteWhere()
	knowledgeArgs := append([]any{item.Namespace, flowID}, knwArgs...)
	rows, err := tx.QueryContext(ctx, `SELECT current_revision_id FROM knw_document WHERE namespace_id=?1 AND current_revision_id IS NOT NULL AND status<>'DELETED' AND (scope='namespace' OR(scope='flow' AND flow_id=?2)) AND (`+knwScope+`)`, knowledgeArgs...)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO orh_run(id,namespace_id,flow_id,flow_revision_id,status,input,output,error,run_instruction,summarize_enabled,context_snapshot,trigger_type,trigger_source,trigger_payload,runtime_mode,runtime_cluster_id,started_at,finished_at,description,created_by,updated_by,metadata) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Namespace, flowID, revisionID, item.Status, stringOrNil(runJSON(item.Input)), stringOrNil(runJSON(item.Output)), stringOrNil(runError(item.Error)), item.RunInstruction, effective, string(runJSON(snapshot)), item.TriggerType, item.TriggerSource, stringOrNil(runJSON(item.TriggerPayload)), item.RuntimeMode, item.RuntimeClusterID, item.StartedAt, item.FinishedAt, item.Description, principal, principal, stringOrNil(runJSON(item.Metadata)))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
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
func nullableString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
func stringOrNil(value []byte) any {
	if value == nil {
		return nil
	}
	return string(value)
}
func (s *FlowRunSQLiteStore) Update(ctx context.Context, item *entities.FlowRunInfo) error {
	item.NormalizeAliases()
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	args := append([]any{item.Status, stringOrNil(runJSON(item.Input)), stringOrNil(runJSON(item.Output)), stringOrNil(runError(item.Error)), item.StartedAt, item.FinishedAt, item.ID}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE orh_run SET status=?1,input=COALESCE(?2,input),output=?3,error=?4,started_at=?5,finished_at=?6 WHERE id=?7 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
func (s *FlowRunSQLiteStore) Delete(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, "DELETED")
}
func (s *FlowRunSQLiteStore) Cancel(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, string(entities.RunCancelled))
}
func (s *FlowRunSQLiteStore) setStatus(ctx context.Context, id, status string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "orh_run").SQLiteWhere()
	args := append([]any{status, id}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE orh_run SET status=?1 WHERE id=?2 AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("run not found or outside authorization scope")
	}
	return nil
}
