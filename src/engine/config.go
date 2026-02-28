package engine

import (
	"time"

	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/queue"
)

// TaskManagerConfig configures a TaskManager.
type TaskManagerConfig struct {
	ID                string
	SlotCount         int
	Queue             queue.Queue
	Store             Store
	Agents            []*config.AgentDef
	MCPClients        map[string]MCPClient
	LLMClient         LLMClient
	Logger            *utils.Logger
	HeartbeatInterval time.Duration
	// SandboxQueue is the dedicated queue for dispatching to sandbox workers.
	// When nil, sandbox plans execute inline.
	SandboxQueue   queue.Queue
	// SandboxPolicy is the global sandbox security policy.
	SandboxPolicy  *model.SandboxPolicy
}
