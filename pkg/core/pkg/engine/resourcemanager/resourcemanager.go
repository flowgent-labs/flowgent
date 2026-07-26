package resourcemanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/cache/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
)

// ─── ResourceManager interface ─────────────────────────────────

// ResourceManager is the single entry point for task dispatch. The JM calls
// Schedule() and the RM internally handles capacity, TM selection via placement
// strategy, and deployment to a physical TM slot. Analogous to Flink's SchedulerNG.
//
// Implementations:
//   - StandaloneResourceManager  (in-process goroutine pool)
//   - KubernetesResourceManager (K8s Deployment + MQTT dispatch)
type ResourceManager interface {
	Provider() engine.Provider
	Validate(ctx context.Context) error
	Schedule(ctx context.Context, plan *entities.ExecutionPlan) (*entities.TaskResult, error)
	Shutdown(ctx context.Context) error
}

// ResourceManagerConfig configures the resource manager.
type ResourceManagerConfig struct {
	Provider      engine.Provider
	SlotsPerTM    int
	MinTMs        int
	MaxTMs        int
	IdleTimeout   time.Duration
	ScaleInterval time.Duration
	PoolSize      int

	Messager      messager.IMessager
	Cache         cache.ICache
	TaskState     taskmanager.TaskStateStore
	ApprovalInfo executor.HumanApprovalStore
	Logger        *utils.Logger

	K8sNamespace      string
	K8sDeploymentName string
	K8sKubeConfigPath string
	TMImage           string
	PlanTimeout       time.Duration

	// Sandbox deployment settings (for K8sRM in distributed mode)
	SandboxEnabled        bool
	SandboxImage          string
	SandboxDeploymentName string
	SandboxMinReplicas    int
	SandboxMaxReplicas    int
	SandboxSlotsPerPod    int
	SandboxResources      *model.SandboxResources
	SandboxWorkspace      string
	SandboxPolicy         *model.SandboxPolicy
	MQTTBroker            string
	PostgresDSN           string
	APIServerURL          string // API server URL for TM pod env var (K8s mode)
	Namespace                string // default namespace for TM runtime resolution
}

// ─── Factory ──────────────────────────────────────────────────

// NewResourceManager creates the configured resource manager implementation.
// If the requested provider fails to initialize (e.g., K8s unreachable), falls
// back to StandaloneResourceManager so the caller always gets a valid RM.
func NewResourceManager(cfg *ResourceManagerConfig) (ResourceManager, error) {
	switch cfg.Provider {
	case engine.ProviderKubernetes:
		rm, err := NewKubernetesResourceManager(cfg)
		if err != nil {
			slog.Warn("kubernetes rm init failed, falling back to local", "err", err)
			return NewStandaloneResourceManager(cfg)
		}
		if cfg.Messager != nil {
			rm.SetQueue(cfg.Messager)
		}
		return rm, nil
	case engine.ProviderStandalone:
		return NewStandaloneResourceManager(cfg)
	default:
		return NewStandaloneResourceManager(cfg)
	}
}

// ─── Compile-time checks ──────────────────────────────────────

var _ ResourceManager = (*StandaloneResourceManager)(nil)
var _ ResourceManager = (*KubernetesResourceManager)(nil)

// ─── Validation ───────────────────────────────────────────────

// ValidateComponents checks cross-component compatibility. Returns fatal errors.
func ValidateComponents(rm ResourceManager) []error {
	var errs []error
	if rm == nil {
		return append(errs, fmt.Errorf("resource manager is nil"))
	}
	if err := rm.Validate(context.Background()); err != nil {
		errs = append(errs, err)
	}
	return errs
}
