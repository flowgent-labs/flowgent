package noderun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
)

// PlanToNodeRun converts an ExecutionPlan to a NodeRun for persistence.
// ExecutionPlans are the runtime unit of work; NodeRun is the persisted entity.
// The conversion maps PlanID → ExecutionID, NodeRunID → ID.
func PlanToNodeRun(plan *entities.ExecutionPlan) *entities.NodeRun {
	return entities.NodeRunFromPlan(plan)
}

type nodeRecord struct {
	GraphRunID       string                 `db:"graph_run_id"`
	NodeInstanceID   string                 `db:"node_instance_id"`
	Attempt          int                    `db:"attempt"`
	AgentRevisionID  *string                `db:"agent_revision_id"`
	Status           entities.NodeRunStatus `db:"status"`
	Input            map[string]any         `db:"input"`
	Output           map[string]any         `db:"output"`
	Error            map[string]any         `db:"error"`
	ExecutionMemory  map[string]any         `db:"execution_memory"`
	Checkpoint       map[string]any         `db:"checkpoint"`
	WorkspaceVersion *string                `db:"workspace_version"`
	ParentNodeRunID  *string                `db:"parent_node_run_id"`
	ExecutionID      string                 `db:"execution_id"`
	LeaseOwner       *string                `db:"lease_owner"`
	LeaseExpiresAt   *time.Time             `db:"lease_expires_at"`
	FencingToken     int64                  `db:"fencing_token"`
	LastHeartbeatAt  *time.Time             `db:"last_heartbeat_at"`
	StartedAt        *time.Time             `db:"started_at"`
	FinishedAt       *time.Time             `db:"finished_at"`
	ID               string                 `db:"id"`
	Description      *string                `db:"description"`
	Namespace        string                 `db:"namespace_id"`
	CreatedAt        time.Time              `db:"created_at"`
	CreatedBy        *string                `db:"created_by"`
	UpdatedAt        time.Time              `db:"updated_at"`
	UpdatedBy        *string                `db:"updated_by"`
	RowVersion       int64                  `db:"row_version"`
	Metadata         map[string]any         `db:"metadata"`
}

func (r *nodeRecord) entity() *entities.NodeRun {
	item := &entities.NodeRun{BaseEntity: entities.BaseEntity{ID: r.ID, Description: taskStringValue(r.Description), Namespace: r.Namespace, Status: string(r.Status), CreatedAt: r.CreatedAt, CreatedBy: taskStringValue(r.CreatedBy), UpdatedAt: r.UpdatedAt, UpdatedBy: taskStringValue(r.UpdatedBy), RowVersion: r.RowVersion, Metadata: r.Metadata}, GraphRunID: r.GraphRunID, NodeInstanceID: r.NodeInstanceID, Attempt: r.Attempt, AgentRevisionID: taskStringValue(r.AgentRevisionID), Status: r.Status, Input: r.Input, Output: r.Output, ExecutionMemory: r.ExecutionMemory, Checkpoint: r.Checkpoint, WorkspaceVersion: taskStringValue(r.WorkspaceVersion), ParentNodeRunID: taskStringValue(r.ParentNodeRunID), ExecutionID: r.ExecutionID, LeaseOwner: taskStringValue(r.LeaseOwner), LeaseExpiresAt: r.LeaseExpiresAt, FencingToken: r.FencingToken, LastHeartbeatAt: r.LastHeartbeatAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if value, ok := r.Error["message"].(string); ok {
		item.Error = value
	}
	if raw, ok := r.Metadata["workspace_snapshot"]; ok {
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, &item.WorkspaceSnapshot)
	}
	if revision, ok := r.Metadata["graph_revision"].(float64); ok {
		item.GraphRevision = entities.GraphRevision(revision)
	}
	item.SubGraphInstanceID, _ = r.Metadata["subgraph_instance_id"].(string)
	item.GraphNodeID, _ = r.Metadata["graph_node_id"].(string)
	item.NodeInstanceID, _ = r.Metadata["node_instance_id"].(string)
	if epoch, ok := r.Metadata["dispatch_epoch"].(float64); ok {
		item.DispatchEpoch = int64(epoch)
	}
	if value, ok := r.Metadata["max_retries"].(float64); ok {
		item.MaxRetries = int(value)
	}
	if value, ok := r.Metadata["sequence"].(float64); ok {
		item.Sequence = int(value)
	}
	return item
}

func taskStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func taskJSON(value any) []byte {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return data
}
func taskError(value string) []byte {
	if value == "" {
		return nil
	}
	return taskJSON(map[string]any{"message": value})
}

// INodeRunStore is the noderun run entity store interface.
type INodeRunStore interface {
	Get(ctx context.Context, id string) (*entities.NodeRun, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.NodeRun], error)
	Save(ctx context.Context, entity *entities.NodeRun) error
	Delete(ctx context.Context, id string) error
	GetByExecutionID(ctx context.Context, execID string) (*entities.NodeRun, error)
	CreateNodeRun(ctx context.Context, entity *entities.NodeRun) error
	UpdateNodeRun(ctx context.Context, entity *entities.NodeRun) error
	ListByGraphRun(ctx context.Context, graphRunID string) ([]*entities.NodeRun, error)
}

// ReadCheckpointNodes is called only while the owning GraphRun is locked. It
// reads the same ledger as the normal repository; no checkpoint business table
// or second execution state machine is introduced.
func ReadCheckpointNodes(ctx context.Context, tx *storage.SQLTransaction, graphRunID string) ([]entities.NodeRun, error) {
	rows, err := tx.Query(ctx, `SELECT `+utils.Columns[nodeRecord]()+` FROM orh_node_run WHERE graph_run_id=? ORDER BY node_instance_id,attempt`, graphRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []entities.NodeRun{}
	for rows.Next() {
		var record nodeRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			return nil, err
		}
		nodes = append(nodes, *record.entity())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	var total int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM orh_node_run WHERE graph_run_id=?`, graphRunID).Scan(&total); err != nil {
		return nil, err
	}
	if total != len(nodes) {
		return nil, storage.ErrFlowgentSqlScopeDenied
	}
	return nodes, nil
}
