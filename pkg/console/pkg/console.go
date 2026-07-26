package console

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/agent"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
	"github.com/flowgent-labs/flowgent/store/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/store/pkg/llmprovider"
	"github.com/flowgent-labs/flowgent/store/pkg/mcp"
	"github.com/flowgent-labs/flowgent/store/pkg/notifier"
	payments "github.com/flowgent-labs/flowgent/wallet/pkg"
	walletproviders "github.com/flowgent-labs/flowgent/wallet/pkg/providers"
)

// FlowgentConsole is the unified resource management class for import/export
// and CRUD of all Flowgent resource kinds (agents, flows, MCPs, LLM providers,
// channels, skills, runs, wallets).
type FlowgentConsole struct {
	store       store.IStore
	secretStore payments.SecretStoreProvider
	namespace      string
	ctx         context.Context
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
	storeImpl := store.InitStore(cfg)

	fc := &FlowgentConsole{
		store: storeImpl,
		ctx:   context.Background(),
	}

	if cfg.Runtime.Namespace.DefaultNamespace != "" {
		fc.namespace = cfg.Runtime.Namespace.DefaultNamespace
	}

	fc.initSecretStore(cfg)
	return fc, nil
}

func (fc *FlowgentConsole) initSecretStore(cfg *config.FlowgentConfig) {
	if cfg.Wallet == nil {
		return
	}
	mkf := cfg.Wallet.SecretStore.MasterKeyFile
	if mkf == "" {
		return
	}
	db, ok := fc.store.DB().(*sql.DB)
	if !ok {
		return
	}
	ss, err := walletproviders.NewDefaultSecretStoreProvider(db, mkf)
	if err != nil {
		slog.Warn("secret store init failed (wallet commands unavailable)", "err", err)
		return
	}
	fc.secretStore = ss
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
