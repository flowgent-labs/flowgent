package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type AgentSQLiteStore struct{ conn *sql.DB }

func NewAgentSQLiteStore(conn *sql.DB) *AgentSQLiteStore { return &AgentSQLiteStore{conn: conn} }

func (s *AgentSQLiteStore) Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[agentRecord]() + ` FROM (
		SELECT a.name,r.revision,r.soul,r.instruction,r.model_config,r.input_schema,r.output_schema,
			a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata
		FROM (SELECT * FROM llm_agent WHERE namespace_id=?1 AND name=?2 AND status<>'DELETED' AND (` + scopeWhere + `)) a
		JOIN llm_agent_revision r ON r.id=a.current_revision_id) current_agent`
	var record agentRecord
	if err := utils.ScanStruct(s.conn.QueryRowContext(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}

func (s *AgentSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").SQLiteWhere()
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_agent WHERE namespace_id=?1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := `SELECT ` + utils.Columns[agentRecord]() + ` FROM (
		SELECT a.name,r.revision,r.soul,r.instruction,r.model_config,r.input_schema,r.output_schema,
			a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata
		FROM (SELECT * FROM llm_agent WHERE namespace_id=?1 AND status<>'DELETED' AND (` + scopeWhere + `)) a
		JOIN llm_agent_revision r ON r.id=a.current_revision_id) current_agent ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.AgentInfo, 0)
	for rows.Next() {
		var record agentRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			return nil, err
		}
		items = append(items, record.entity())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, req), nil
}

func (s *AgentSQLiteStore) Save(ctx context.Context, item *entities.AgentInfo) error {
	if item.Namespace == "" {
		item.Namespace = "default"
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	principal := item.UpdatedBy
	if principal == "" {
		principal = item.CreatedBy
	}
	if principal == "" {
		principal = "system:flowgent"
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, item.Namespace, item.Namespace, "Flowgent namespace", principal, principal); err != nil {
		return err
	}
	var id string
	var rowVersion int64
	err = tx.QueryRowContext(ctx, `SELECT id,row_version FROM llm_agent WHERE namespace_id=? AND (id=? OR name=?)`, item.Namespace, item.ID, item.Name).Scan(&id, &rowVersion)
	if errors.Is(err, sql.ErrNoRows) {
		id = item.ID
		rowVersion = 1
		if _, err = tx.ExecContext(ctx, `INSERT INTO llm_agent(id,namespace_id,name,description,status,created_by,updated_by,metadata) VALUES(?,?,?,?,'ACTIVE',?,?,?)`, id, item.Namespace, item.Name, item.Description, principal, principal, string(jsonBytes(item.Metadata))); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM llm_agent_revision WHERE agent_id=?`, id).Scan(&revision); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO llm_agent_revision(id,namespace_id,agent_id,revision,soul,instruction,model_config,input_schema,output_schema,description,status,created_by,updated_by) VALUES(?,?,?,?,?,?,?,?,?,?,'PUBLISHED',?,?)`, revisionID, item.Namespace, id, revision, item.Soul, item.Instruction, string(jsonBytes(agentModelConfig(item))), string(jsonBytes(item.InputSchema)), string(jsonBytes(item.OutputSchema)), item.Description, principal, principal); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE llm_agent SET name=?,description=?,current_revision_id=?,status='ACTIVE',updated_by=?,metadata=? WHERE id=? AND namespace_id=? AND row_version=?`, item.Name, item.Description, revisionID, principal, string(jsonBytes(item.Metadata)), id, item.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("agent revision CAS conflict")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item.ID = id
	item.Revision = revision
	item.Version = revision
	return nil
}

func (s *AgentSQLiteStore) Delete(ctx context.Context, namespace, name string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE llm_agent SET status='DELETED' WHERE namespace_id=?1 AND name=?2 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("agent not found or outside authorization scope")
	}
	return nil
}
