package resourcemanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/cache/pkg"
	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/engine"
	"github.com/flowgent-labs/flowgent/model/pkg"
	messager "github.com/flowgent-labs/flowgent/messager/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg"
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
	Schedule(ctx context.Context, plan *model.ExecutionPlan) (*model.TaskResult, error)
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

	Queue messager.IMessager
	Cache cache.ICache
	Store store.IStore
	Agents     []*config.AgentDef
	MCPClients map[string]engine.MCPClient
	LLMClient  engine.LLMClient
	Logger     *utils.Logger

	K8sNamespace      string
	K8sDeploymentName string
	K8sKubeConfigPath string
	AutoScale         bool // true=application mode (JM auto-scales TMs), false=session (admin-managed)
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
		if cfg.Queue != nil {
			rm.SetQueue(cfg.Queue)
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
func ValidateComponents(rm ResourceManager, store store.IStore) []error {
	var errs []error
	if rm == nil {
		return append(errs, fmt.Errorf("resource manager is nil"))
	}
	if store == nil {
		return append(errs, fmt.Errorf("store is nil"))
	}
	if err := rm.Validate(context.Background()); err != nil {
		errs = append(errs, err)
	}
	// Note: store.DB() returns nil for some implementations (e.g., Postgres via pgx).
	// The actual store connectivity is verified at runtime when queries are executed.
	return errs
}

// WarnCompatibility logs warnings for unusual but non-fatal configurations.
func WarnCompatibility(rm ResourceManager, store store.IStore) {
	if rm == nil || store == nil {
		return
	}
	switch rm.Provider() {
	case engine.ProviderStandalone:
		if store.DB() != nil {
			slog.Warn("local rm with postgres — consider SQLite for all-in-one mode")
		}
	case engine.ProviderKubernetes:
		if store.DB() == nil {
			slog.Warn("kubernetes rm with in-memory store — needs postgres for consistency")
		}
	}
}
