package entities

import "time"

// GraphRevision identifies a committed runtime revision of one SubGraph.
type GraphRevision int64

// WorkspaceSnapshot identifies an immutable, checksummed file manifest on the
// configured durable workspace volume. It contains no process or credentials.
type WorkspaceSnapshot struct {
	ID         string    `json:"id"`
	Ref        string    `json:"ref"`
	SHA256     string    `json:"sha256"`
	BaseCommit string    `json:"base_commit,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// GraphCheckpoint is a consistent recovery cut. NodeRuns and Outbox remain the
// authoritative committed ledger after this sequence; a checkpoint accelerates
// restoration and never authorizes repeating an external effect.
type GraphCheckpoint struct {
	ID           string               `json:"id"`
	GraphRunID   string               `json:"graph_run_id"`
	EventSeq     int64                `json:"event_seq"`
	StateVersion int64                `json:"state_version"`
	Snapshot     AgentGraph           `json:"snapshot"`
	SubGraphs    map[string]*SubGraph `json:"subgraphs,omitempty"`
	NodeRuns     []NodeRun            `json:"node_runs"`
	SHA256       string               `json:"sha256"`
	CreatedAt    time.Time            `json:"created_at"`
}

// GraphRecovery overlays committed state after the selected checkpoint. It is
// a response projection of the existing ledger, not another execution engine.
type GraphRecovery struct {
	Checkpoint          *GraphCheckpoint     `json:"checkpoint,omitempty"`
	Snapshot            AgentGraph           `json:"snapshot"`
	SubGraphs           map[string]*SubGraph `json:"subgraphs,omitempty"`
	NodeRuns            []NodeRun            `json:"node_runs"`
	RejectedCheckpoints []string             `json:"rejected_checkpoints,omitempty"`
}
