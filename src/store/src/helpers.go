package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/model/src"
)

// toJSON marshals v to JSON bytes.
func toJSON(v any) []byte {
	if v == nil {
		return []byte("null")
	}
	b, _ := json.Marshal(v)
	return b
}

// newUUID generates a time-based UUID string.
func newUUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		time.Now().UnixNano()&0xFFFFFFFF, (time.Now().UnixNano()>>32)&0xFFFF,
		(time.Now().UnixNano()>>48)&0xFFFF, (time.Now().UnixNano()>>48)&0xFFFF|0x4000,
		time.Now().UnixNano()&0xFFFFFFFFFFFF)
}

// ─── Scan helpers ──────────────────────────────────────────────

func scanAgentFlowVersion(s scanner) (*model.AgentFlowVersion, error) {
	var d model.AgentFlowVersion
	var b []byte
	if err := s.Scan(&d.AgentFlowID, &d.Version, &b, &d.CreatedBy, &d.Comment, &d.CreatedAt); err != nil {
		return nil, err
	}
	d.Definition = b
	return &d, nil
}

func scanAgentFlowVersionRow(r rowsScanner) (*model.AgentFlowVersion, error) {
	return scanAgentFlowVersion(r)
}

func scanAgentFlowRun(s scanner) (*model.AgentFlowRun, error) {
	var r model.AgentFlowRun
	var varsB, outB, triggerPayload []byte
	var errStr sql.NullString
	var startedAt, finishedAt sql.NullTime
	if err := s.Scan(&r.ID, &r.AgentFlowID, &r.Version, &r.Status, &varsB, &outB, &errStr, &r.Trigger.Type, &r.Trigger.Source, &triggerPayload, &r.CreatedAt, &r.UpdatedAt, &r.TenantID, &r.Namespace, &r.Priority, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if errStr.Valid {
		r.Error = errStr.String
	}
	if varsB != nil {
		json.Unmarshal(varsB, &r.Vars)
	}
	if outB != nil {
		json.Unmarshal(outB, &r.Output)
	}
	if triggerPayload != nil {
		json.Unmarshal(triggerPayload, &r.Trigger.Payload)
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	return &r, nil
}

func scanAgentFlowRunRow(r rowsScanner) (*model.AgentFlowRun, error) {
	return scanAgentFlowRun(r)
}

func scanTaskRun(s scanner) (*model.TaskRun, error) {
	var t model.TaskRun
	var inputB, outB []byte
	var startedAt, finishedAt sql.NullTime
	var parentID sql.NullString
	if err := s.Scan(&t.ID, &t.AgentFlowRunID, &t.NodeID, &t.Status, &inputB, &outB, &t.Error, &t.RetryCount, &t.MaxRetries, &t.ExecID, &parentID, &t.Sequence, &t.CreatedAt, &t.UpdatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if inputB != nil {
		json.Unmarshal(inputB, &t.Input)
	}
	if outB != nil {
		json.Unmarshal(outB, &t.Output)
	}
	if parentID.Valid {
		t.ParentTaskRunID = parentID.String
	}
	if startedAt.Valid {
		t.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		t.FinishedAt = &finishedAt.Time
	}
	return &t, nil
}

func scanTaskRunRow(r rowsScanner) (*model.TaskRun, error) {
	return scanTaskRun(r)
}

func scanHumanApproval(s scanner) (*model.HumanApproval, error) {
	var a model.HumanApproval
	var approved sql.NullBool
	var comment sql.NullString
	var expiresAt, resolvedAt sql.NullTime
	var timeoutSecs int
	if err := s.Scan(&a.TaskRunID, &a.Token, &a.Status, &approved, &comment, &timeoutSecs, &a.CreatedAt, &a.UpdatedAt, &expiresAt, &resolvedAt); err != nil {
		return nil, err
	}
	if approved.Valid {
		a.Approved = &approved.Bool
	}
	if comment.Valid {
		a.Comment = comment.String
	}
	a.Timeout = time.Duration(timeoutSecs) * time.Second
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	return &a, nil
}

func scanHumanApprovalRow(r rowsScanner) (*model.HumanApproval, error) {
	return scanHumanApproval(r)
}
