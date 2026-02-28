package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/src/common/utils"
	"github.com/flowgent-labs/flowgent/src/config"
	"github.com/flowgent-labs/flowgent/src/engine"
	"github.com/flowgent-labs/flowgent/src/model"
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
	Provider       engine.Provider
	SlotsPerTM     int
	MinTMs         int
	MaxTMs         int
	IdleTimeout    time.Duration
	ScaleInterval  time.Duration
	PoolSize       int

	Store      engine.Store
	Agents     []*config.AgentDef
	MCPClients map[string]engine.MCPClient
	LLMClient  engine.LLMClient
	Logger     *utils.Logger

	K8sNamespace      string
	K8sDeploymentName string
	K8sKubeConfigPath string
	TMImage           string
	PlanTimeout       time.Duration
}

// ─── Factory ──────────────────────────────────────────────────

// NewResourceManager creates the configured resource manager implementation.
func NewResourceManager(cfg *ResourceManagerConfig) (ResourceManager, error) {
	switch cfg.Provider {
	case engine.ProviderLocal:
		return NewLocalResourceManager(cfg)
	case engine.ProviderKubernetes:
		return NewKubernetesResourceManager(cfg)
	default:
		return NewLocalResourceManager(cfg)
	}
}

// ─── Compile-time checks ──────────────────────────────────────

var _ ResourceManager = (*LocalResourceManager)(nil)
var _ ResourceManager = (*KubernetesResourceManager)(nil)

// ─── Validation ───────────────────────────────────────────────

// ValidateComponents checks cross-component compatibility. Returns fatal errors.
func ValidateComponents(rm ResourceManager, store engine.Store) []error {
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
	switch rm.Provider() {
	case engine.ProviderKubernetes:
		if store.DB() == nil {
			errs = append(errs, fmt.Errorf("kubernetes rm requires postgres, got in-memory store"))
		}
	}
	return errs
}

// WarnCompatibility logs warnings for unusual but non-fatal configurations.
func WarnCompatibility(rm ResourceManager, store engine.Store) {
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
