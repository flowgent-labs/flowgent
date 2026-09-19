package resourcemanager

import (
	"context"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/cache/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/executor"
	"github.com/flowgent-labs/flowgent/core/pkg/engine/taskmanager"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
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

	Messager     messager.IMessager
	Cache        cache.ICache
	TaskState    taskmanager.TaskStateStore
	ApprovalInfo executor.HumanApprovalStore
	Logger       *utils.Logger

	K8sNamespace             string
	K8sDeploymentName        string
	K8sKubeConfigPath        string
	ConfigMapName            string
	TMImage                  string
	TMResources              *model.SandboxResources
	PriorityClassName        string
	NodeSelector             map[string]string
	PlanTimeout              time.Duration
	OwnerNamespaceID         string
	RuntimeClusterID         string
	OwnerFlowID              string
	OwnerRunID               string
	RuntimeMode              entities.RuntimeMode
	OwnerJobManagerName      string
	OwnerJobManagerNamespace string
	ResourceOwner            string
	DeleteOnShutdown         bool

	// Sandbox deployment settings (for K8sRM in distributed mode)
	SandboxEnabled        bool
	SandboxImage          string
	SandboxDeploymentName string
	SandboxMinReplicas    int
	SandboxMaxReplicas    int
	SandboxSlotsPerPod    int
	SandboxResources      *model.SandboxResources
	SandboxWorkspace      string
	SandboxHostWorkspace  string
	SandboxPolicy         *model.SandboxPolicy
	MQTTBroker            string
	PostgresDSN           string
	APIServerURL          string // API server URL for TM pod env var (K8s mode)
	Namespace             string // default namespace for TM runtime resolution
	CredentialEnvSecret   string // optional K8s Secret mounted via envFrom into TM/Sandbox pods
	InternalAuthSecret    string // K8s Secret containing the TaskManager workload credential
	TaskManagerAuthKey    string // key in InternalAuthSecret; defaults to taskmanager-token
}

// ─── Factory ──────────────────────────────────────────────────

// NewResourceManager creates exactly the configured resource manager.
// Initialization errors are fatal because silently changing execution backends
// would violate runtime cluster isolation and scheduling guarantees.
func NewResourceManager(cfg *ResourceManagerConfig) (ResourceManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("resource manager config is required")
	}
	switch cfg.Provider {
	case engine.ProviderKubernetes:
		rm, err := NewKubernetesResourceManager(cfg)
		if err != nil {
			return nil, fmt.Errorf("initialize kubernetes resource manager: %w", err)
		}
		if cfg.Messager != nil {
			rm.SetQueue(cfg.Messager)
		}
		return rm, nil
	case engine.ProviderStandalone:
		return NewStandaloneResourceManager(cfg)
	default:
		return nil, fmt.Errorf("unsupported resource manager provider %q", cfg.Provider)
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
