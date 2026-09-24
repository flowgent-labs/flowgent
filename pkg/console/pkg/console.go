package console

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/agent"
	"github.com/flowgent-labs/flowgent/storage/pkg/flow"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/llmprovider"
	"github.com/flowgent-labs/flowgent/storage/pkg/mcp"
	"github.com/flowgent-labs/flowgent/storage/pkg/notifier"
)

// FlowgentConsole is the unified resource management class for import/export
// and CRUD of all Flowgent resource kinds (agents, flows, MCPs, LLM providers,
// channels, skills, and runs). Wallet keys are owned by the external Wallet
// service and are managed with walletd, outside the Flowgent process.
type FlowgentConsole struct {
	store           storage.IStorage
	namespace       string
	ctx             context.Context
	secretCipher    secretbox.ISecretCipher
	secretCipherErr error
}

// lazyStores holds lazily-initialized per-entity stores.
type lazyStores struct {
	agents   agent.IAgentInfoStore
	flows    flow.IFlowInfoStore
	runs     flowrun.IFlowRunStore
	channels notifier.INotifierStore
	llm      llmprovider.ILlmProviderStore
	mcps     mcp.IMCPStore
}

// NewFlowgentConsole creates a FlowgentConsole from the given config.
func NewFlowgentConsole(cfg *config.FlowgentConfig) (*FlowgentConsole, error) {
	storeImpl := storage.InitStorage(cfg)

	fc := &FlowgentConsole{
		store: storeImpl,
		ctx:   context.Background(),
	}
	if encryption := cfg.Notifier.SecretEncryption; encryption.Provider != "" {
		if encryption.Provider != "aesgcm" {
			fc.secretCipherErr = fmt.Errorf("notification secret encryption provider must be aesgcm")
		} else {
			fc.secretCipher, fc.secretCipherErr = secretbox.NewAESGCMSecretCipher(
				encryption.ActiveKeyID,
				encryption.Keys,
			)
		}
	}

	if cfg.Runtime.Namespace.DefaultNamespace != "" {
		fc.namespace = cfg.Runtime.Namespace.DefaultNamespace
	}

	return fc, nil
}

func (fc *FlowgentConsole) protectChannelSecrets(ch *entities.NotifyChannelInfo) error {
	if fc.secretCipherErr != nil {
		return fmt.Errorf("notification secret encryption is unavailable: %w", fc.secretCipherErr)
	}
	if fc.secretCipher == nil {
		return fmt.Errorf("notification secret encryption is not configured")
	}
	return ch.ProtectSecrets(fc.ctx, fc.secretCipher)
}

// SetNamespace sets the active namespace.
func (fc *FlowgentConsole) SetNamespace(namespace string) { fc.namespace = namespace }

// Namespace returns the active namespace.
func (fc *FlowgentConsole) Namespace() string { return fc.namespace }

// DB returns the underlying database handle for direct store access.
func (fc *FlowgentConsole) DB() any { return fc.store.DB() }

// Close closes the underlying store.
func (fc *FlowgentConsole) Close() error {
	if closer, ok := fc.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (fc *FlowgentConsole) getStores() *lazyStores {
	ls := &lazyStores{}
	switch db := fc.store.DB().(type) {
	case *pgxpool.Pool:
		ls.agents = agent.NewAgentPostgresStore(db)
		ls.flows = flow.NewFlowPostgresStore(db)
		ls.runs = flowrun.NewFlowRunPostgresStore(db)
		ls.channels = notifier.NewNotifierPostgresStore(db)
		ls.llm = llmprovider.NewLlmProviderPostgresStore(db)
		ls.mcps = mcp.NewMCPPostgresStore(db)
	case *sql.DB:
		ls.agents = agent.NewAgentSQLiteStore(db)
		ls.flows = flow.NewFlowSQLiteStore(db)
		ls.runs = flowrun.NewFlowRunSQLiteStore(db)
		ls.channels = notifier.NewNotifierSQLiteStore(db)
		ls.llm = llmprovider.NewLlmProviderSQLiteStore(db)
		ls.mcps = mcp.NewMCPSQLiteStore(db)
	}
	return ls
}
