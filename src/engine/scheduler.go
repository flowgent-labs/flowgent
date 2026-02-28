package engine

import (
	"context"
	"time"

	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/util"
)

// SchedulerType identifies the scheduling backend.
type SchedulerType string

const (
	SchedulerTypeLocal      SchedulerType = "local"
	SchedulerTypeKubernetes SchedulerType = "kubernetes"
)

// Scheduler dispatches ExecutionPlans to TaskManager workers.
// JM calls EnsureCapacity before dispatching, then SubmitTask for each plan.
// Implementations:
//   - LocalScheduler: goroutine pool in same process
//   - KubernetesScheduler: scale K8s Deployment + dispatch via MQTT
type Scheduler interface {
	Type() SchedulerType

	// EnsureCapacity guarantees at least neededSlots TM slots are available.
	// Returns the number of slots now available.
	EnsureCapacity(ctx context.Context, neededSlots int) (int, error)

	// SubmitTask sends an ExecutionPlan for execution and blocks until
	// the result is available (or returns error on dispatch failure).
	SubmitTask(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error)

	// AvailableSlots returns the current number of idle slots.
	AvailableSlots() int

	// TotalSlots returns the total slot capacity.
	TotalSlots() int

	// Shutdown releases scheduler resources.
	Shutdown(ctx context.Context) error
}

// SchedulerConfig configures the scheduler.
type SchedulerConfig struct {
	Type         SchedulerType
	SlotsPerTM   int
	MinTMs       int
	MaxTMs       int
	IdleTimeout  time.Duration
	PoolSize     int

	// TM dependencies (used by LocalScheduler to create TM internally)
	Store      Store
	Agents     []*config.AgentDef
	MCPClients map[string]MCPClient
	LLMClient  LLMClient
	Logger     *util.Logger

	// KubernetesScheduler specific
	K8sNamespace      string
	K8sDeploymentName string
	K8sKubeConfigPath string
	TMImage           string
}

// NewScheduler creates the configured scheduler implementation.
func NewScheduler(cfg *SchedulerConfig) (Scheduler, error) {
	switch cfg.Type {
	case SchedulerTypeLocal:
		return NewLocalScheduler(cfg)
	case SchedulerTypeKubernetes:
		return NewKubernetesScheduler(cfg)
	default:
		// Default to local for safety
		return NewLocalScheduler(cfg)
	}
}
