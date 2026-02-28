package resourcemanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/config/src"
	"github.com/flowgent-labs/flowgent/core/src/engine"
	"github.com/flowgent-labs/flowgent/model/src"
	"github.com/flowgent-labs/flowgent/messaging/src"
	"github.com/flowgent-labs/flowgent/store/src"
)

// ─── ResourceManager interface ─────────────────────────────────

// ResourceManager is the single entry point for task dispatch. The JM calls
// Schedule() and the RM internally handles capacity, TM selection via placement
// strategy, and deployment to a physical TM slot. Analogous to Flink's SchedulerNG.
//
// Implementations:
//   - LocalResourceManager  (in-process goroutine pool)
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

	Queue      queue.Queue
	Store      store.Store
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
}

// ─── Factory ──────────────────────────────────────────────────

// NewResourceManager creates the configured resource manager implementation.
// If the requested provider fails to initialize (e.g., K8s unreachable), falls
// back to LocalResourceManager so the caller always gets a valid RM.
func NewResourceManager(cfg *ResourceManagerConfig) (ResourceManager, error) {
	switch cfg.Provider {
	case engine.ProviderKubernetes:
		rm, err := NewKubernetesResourceManager(cfg)
		if err != nil {
			slog.Warn("kubernetes rm init failed, falling back to local", "err", err)
			return NewLocalResourceManager(cfg)
		}
		if cfg.Queue != nil {
			rm.SetQueue(cfg.Queue)
		}
		return rm, nil
	case engine.ProviderLocal:
		return NewLocalResourceManager(cfg)
	default:
		return NewLocalResourceManager(cfg)
	}
}

// ─── Compile-time checks ──────────────────────────────────────

var _ ResourceManager = (*LocalResourceManager)(nil)
var _ ResourceManager = (*KubernetesResourceManager)(nil)

// ─── Validation ───────────────────────────────────────────────

// ValidateComponents checks cross-component compatibility. Returns fatal errors.
func ValidateComponents(rm ResourceManager, store store.Store) []error {
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
func WarnCompatibility(rm ResourceManager, store store.Store) {
	if rm == nil || store == nil {
		return
	}
	switch rm.Provider() {
	case engine.ProviderLocal:
		if store.DB() != nil {
			slog.Warn("local rm with postgres — consider SQLite for all-in-one mode")
		}
	case engine.ProviderKubernetes:
		if store.DB() == nil {
			slog.Warn("kubernetes rm with in-memory store — needs postgres for consistency")
		}
	}
}
