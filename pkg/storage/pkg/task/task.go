package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// PlanToTaskRun converts an ExecutionPlan to a TaskRunInfo for persistence.
// ExecutionPlans are the runtime unit of work; TaskRunInfo is the persisted entity.
// The conversion maps PlanID → ExecID, TaskID → ID.
func PlanToTaskRun(plan *entities.ExecutionPlan) *entities.TaskRunInfo {
	output := map[string]any{}
	errStr := ""
	if plan.Result != nil {
		output = plan.Result.Output
		errStr = plan.Result.Error
	}
	now := time.Now()
	createdAt := plan.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	return &entities.TaskRunInfo{
		BaseEntity:       entities.BaseEntity{ID: plan.TaskID, CreatedAt: createdAt, UpdatedAt: now},
		RunID:            plan.AgentFlowRunID,
		AgentFlowRunID:   plan.AgentFlowRunID,
		NodeKey:          plan.NodeID,
		NodeID:           plan.NodeID,
		Attempt:          plan.RetryCount + 1,
		Status:           plan.State,
		Input:            plan.Input,
		Output:           output,
		Error:            errStr,
		RetryCount:       plan.RetryCount,
		MaxRetries:       plan.MaxRetries,
		ExecutionID:      fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1),
		ExecID:           fmt.Sprintf("%s-attempt-%d", plan.PlanID, plan.RetryCount+1),
		Checkpoint:       checkpointMap(plan.Checkpoint),
		WorkspaceVersion: plan.WorkspaceVersion,
		FencingToken:     plan.FencingToken,
		StartedAt:        plan.StartedAt,
		FinishedAt:       plan.FinishedAt,
	}
}

func checkpointMap(value *entities.TaskCheckpoint) map[string]any {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

type nodeRecord struct {
	RunID            string              `db:"run_id"`
	NodeKey          string              `db:"node_key"`
	Attempt          int                 `db:"attempt"`
	AgentRevisionID  *string             `db:"agent_revision_id"`
	Status           entities.TaskStatus `db:"status"`
	Input            map[string]any      `db:"input"`
	Output           map[string]any      `db:"output"`
	Error            map[string]any      `db:"error"`
	ExecutionMemory  map[string]any      `db:"execution_memory"`
	Checkpoint       map[string]any      `db:"checkpoint"`
	WorkspaceVersion *string             `db:"workspace_version"`
	ParentNodeRunID  *string             `db:"parent_node_run_id"`
	ExecutionID      string              `db:"execution_id"`
	LeaseOwner       *string             `db:"lease_owner"`
	LeaseExpiresAt   *time.Time          `db:"lease_expires_at"`
	FencingToken     int64               `db:"fencing_token"`
	LastHeartbeatAt  *time.Time          `db:"last_heartbeat_at"`
	StartedAt        *time.Time          `db:"started_at"`
	FinishedAt       *time.Time          `db:"finished_at"`
	ID               string              `db:"id"`
	Description      *string             `db:"description"`
	Namespace        string              `db:"namespace_id"`
	CreatedAt        time.Time           `db:"created_at"`
	CreatedBy        *string             `db:"created_by"`
	UpdatedAt        time.Time           `db:"updated_at"`
	UpdatedBy        *string             `db:"updated_by"`
	RowVersion       int64               `db:"row_version"`
	Metadata         map[string]any      `db:"metadata"`
}

func (r *nodeRecord) entity() *entities.TaskRunInfo {
	item := &entities.TaskRunInfo{BaseEntity: entities.BaseEntity{ID: r.ID, Description: taskStringValue(r.Description), Namespace: r.Namespace, Status: string(r.Status), CreatedAt: r.CreatedAt, CreatedBy: taskStringValue(r.CreatedBy), UpdatedAt: r.UpdatedAt, UpdatedBy: taskStringValue(r.UpdatedBy), RowVersion: r.RowVersion, Metadata: r.Metadata}, RunID: r.RunID, AgentFlowRunID: r.RunID, NodeKey: r.NodeKey, NodeID: r.NodeKey, Attempt: r.Attempt, RetryCount: r.Attempt - 1, AgentRevisionID: taskStringValue(r.AgentRevisionID), Status: r.Status, Input: r.Input, Output: r.Output, ExecutionMemory: r.ExecutionMemory, Checkpoint: r.Checkpoint, WorkspaceVersion: taskStringValue(r.WorkspaceVersion), ParentNodeRunID: taskStringValue(r.ParentNodeRunID), ParentTaskRunID: taskStringValue(r.ParentNodeRunID), ExecutionID: r.ExecutionID, ExecID: r.ExecutionID, LeaseOwner: taskStringValue(r.LeaseOwner), LeaseExpiresAt: r.LeaseExpiresAt, FencingToken: r.FencingToken, LastHeartbeatAt: r.LastHeartbeatAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt}
	if value, ok := r.Error["message"].(string); ok {
		item.Error = value
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

// ITaskStore is the task run entity store interface.
type ITaskStore interface {
	Get(ctx context.Context, id string) (*entities.TaskRunInfo, error)
	Select(ctx context.Context, req entities.PageRequest) (*entities.Page[entities.TaskRunInfo], error)
	Save(ctx context.Context, entity *entities.TaskRunInfo) error
	Delete(ctx context.Context, id string) error
	GetByExecID(ctx context.Context, execID string) (*entities.TaskRunInfo, error)
	CreateTaskRun(ctx context.Context, entity *entities.TaskRunInfo) error
	UpdateTaskRun(ctx context.Context, entity *entities.TaskRunInfo) error
	ListByFlowRun(ctx context.Context, flowRunID string) ([]*entities.TaskRunInfo, error)
}
