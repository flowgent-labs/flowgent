package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AgentPostgresStore struct{ pool *pgxpool.Pool }

func NewAgentPostgresStore(pool *pgxpool.Pool) *AgentPostgresStore {
	return &AgentPostgresStore{pool: pool}
}

func (s *AgentPostgresStore) Get(ctx context.Context, namespace, name string) (*entities.AgentInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").PostgresWhere(3)
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[agentRecord]() + ` FROM (
		SELECT a.name,r.revision,r.soul,r.instruction,r.model_config,r.input_schema,r.output_schema,
			a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata
		FROM (SELECT * FROM llm_agent WHERE namespace_id=$1 AND name=$2 AND status<>'DELETED' AND (` + scopeWhere + `)) a
		JOIN llm_agent_revision r ON r.id=a.current_revision_id
	) current_agent`
	var record agentRecord
	if err := utils.ScanStruct(s.pool.QueryRow(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}

func (s *AgentPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.AgentInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").PostgresWhere(2)
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM llm_agent WHERE namespace_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	pos := len(baseArgs) + 1
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := fmt.Sprintf(`SELECT %s FROM (
		SELECT a.name,r.revision,r.soul,r.instruction,r.model_config,r.input_schema,r.output_schema,
			a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata
		FROM (SELECT * FROM llm_agent WHERE namespace_id=$1 AND status<>'DELETED' AND (%s)) a
		JOIN llm_agent_revision r ON r.id=a.current_revision_id
	) current_agent ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[agentRecord](), scopeWhere, pos, pos+1)
	rows, err := s.pool.Query(ctx, query, args...)
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

func (s *AgentPostgresStore) Save(ctx context.Context, item *entities.AgentInfo) error {
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by) VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255)) ON CONFLICT(id) DO NOTHING`, item.Namespace, principal); err != nil {
		return err
	}
	var id string
	var rowVersion int64
	err = tx.QueryRow(ctx, `SELECT id,row_version FROM llm_agent WHERE namespace_id=$1 AND (id=$2 OR name=$3) FOR UPDATE`, item.Namespace, item.ID, item.Name).Scan(&id, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		id = item.ID
		rowVersion = 1
		if _, err = tx.Exec(ctx, `INSERT INTO llm_agent(id,namespace_id,name,description,status,created_by,updated_by,metadata) VALUES($1,$2,$3,$4,'ACTIVE',$5,$5,$6)`, id, item.Namespace, item.Name, item.Description, principal, jsonBytes(item.Metadata)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM llm_agent_revision WHERE agent_id=$1`, id).Scan(&revision); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO llm_agent_revision(id,namespace_id,agent_id,revision,soul,instruction,model_config,input_schema,output_schema,description,status,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'PUBLISHED',$11,$11)`, revisionID, item.Namespace, id, revision, item.Soul, item.Instruction, jsonBytes(agentModelConfig(item)), jsonBytes(item.InputSchema), jsonBytes(item.OutputSchema), item.Description, principal); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE llm_agent SET name=$1,description=$2,current_revision_id=$3,status='ACTIVE',updated_by=$4,metadata=$5 WHERE id=$6 AND namespace_id=$7 AND row_version=$8`, item.Name, item.Description, revisionID, principal, jsonBytes(item.Metadata), id, item.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("agent revision CAS conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	item.ID = id
	item.Revision = revision
	item.Version = revision
	return nil
}

func (s *AgentPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	scopeWhere, args0 := storage.FlowgentSqlScopeForTable(ctx, "llm_agent").PostgresWhere(3)
	args := append([]any{namespace, name}, args0...)
	result, err := s.pool.Exec(ctx, `UPDATE llm_agent SET status='DELETED' WHERE namespace_id=$1 AND name=$2 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("agent not found or outside authorization scope")
	}
	return nil
}

func jsonBytes(value any) []byte { data, _ := json.Marshal(value); return data }
