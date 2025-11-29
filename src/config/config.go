package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/flowgent-labs/flowgent/src/model"
)

// ─── Top-level config ────────────────────────────────────────

// ServiceConfig is the top-level runtime configuration for the flowgent engine.
type ServiceConfig struct {
	ServiceName   string              `json:"service-name" yaml:"service-name"`
	Server        ServerConfig        `json:"server" yaml:"server"`
	A2A           A2AConfig           `json:"a2a" yaml:"a2a"`
	Mgmt          MgmtConfig          `json:"mgmt" yaml:"mgmt"`
	Logging       LoggingConfig       `json:"logging" yaml:"logging"`
	Auth          AuthConfig          `json:"auth" yaml:"auth"`
	Cache         CacheConfig         `json:"cache" yaml:"cache"`
	Storage       StorageConfig       `json:"storage" yaml:"storage"`
	LLM           LLMConfig           `json:"llm" yaml:"llm"`
	Orchestration OrchestrationConfig `json:"orchestration" yaml:"orchestration"`
}

// ─── Server ──────────────────────────────────────────────────

// A2AConfig configures the Agent-to-Agent (Google A2A protocol) server.
type A2AConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Host    string `json:"host" yaml:"host"`
	Port    int    `json:"port" yaml:"port"`
}

type ServerConfig struct {
	Host            string `json:"host" yaml:"host"`
	Port            int    `json:"port" yaml:"port"`
	ContextPath     string `json:"context-path" yaml:"context-path"`
	ShutdownTimeout string `json:"shutdown-timeout" yaml:"shutdown-timeout"`
	MaxBodyBytes    int    `json:"max-body-bytes" yaml:"max-body-bytes"`
	ReadTimeout     string `json:"read-timeout" yaml:"read-timeout"`
	WriteTimeout    string `json:"write-timeout" yaml:"write-timeout"`
}

type MgmtConfig struct {
	Enabled bool        `json:"enabled" yaml:"enabled"`
	Host    string      `json:"host" yaml:"host"`
	Port    int         `json:"port" yaml:"port"`
	PProf   PProfConfig `json:"pprof" yaml:"pprof"`
	OTEL    OTELConfig  `json:"otel" yaml:"otel"`
}

type PProfConfig struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	ServerBind string `json:"server-bind" yaml:"server-bind"`
}

type OTELConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	Protocol string `json:"protocol" yaml:"protocol"`
	Timeout  int    `json:"timeout" yaml:"timeout"`
}

// ─── Logging / Auth ──────────────────────────────────────────

type LoggingConfig struct {
	Mode  string `json:"mode" yaml:"mode"`
	Level string `json:"level" yaml:"level"`
}

type AuthConfig struct {
	JWTValidityAK  int              `json:"jwt-validity-ak" yaml:"jwt-validity-ak"`
	JWTValidityRK  int              `json:"jwt-validity-rk" yaml:"jwt-validity-rk"`
	JWTAlgorithm   string           `json:"jwt-algorithm" yaml:"jwt-algorithm"`
	JWTPrivateKey  string           `json:"jwt-private-key" yaml:"jwt-private-key"`
	JWTPublicKey   string           `json:"jwt-public-key" yaml:"jwt-public-key"`
	AnonymousPaths []string         `json:"anonymous-paths" yaml:"anonymous-paths"`
	OIDC           OIDCConfig       `json:"oidc" yaml:"oidc"`
	GitHub         GitHubAuthConfig `json:"github" yaml:"github"`
}

type OIDCConfig struct {
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	ClientID    string `json:"client-id" yaml:"client-id"`
	IssueURL    string `json:"issue-url" yaml:"issue-url"`
	RedirectURL string `json:"redirect-url" yaml:"redirect-url"`
	Scope       string `json:"scope" yaml:"scope"`
}

type GitHubAuthConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	ClientID     string `json:"client-id" yaml:"client-id"`
	ClientSecret string `json:"client-secret" yaml:"client-secret"`
	AuthURL      string `json:"auth-url" yaml:"auth-url"`
	TokenURL     string `json:"token-url" yaml:"token-url"`
	RedirectURL  string `json:"redirect-url" yaml:"redirect-url"`
	Scope        string `json:"scope" yaml:"scope"`
	UserInfoURL  string `json:"user-info-url" yaml:"user-info-url"`
}

// ─── Cache ───────────────────────────────────────────────────

type CacheConfig struct {
	Provider string            `json:"provider" yaml:"provider"`
	Memory   MemoryCacheConfig `json:"memory" yaml:"memory"`
	Redis    RedisCacheConfig  `json:"redis" yaml:"redis"`
}

type MemoryCacheConfig struct {
	InitialCapacity int    `json:"initial-capacity" yaml:"initial-capacity"`
	MaxCapacity     int    `json:"max-capacity" yaml:"max-capacity"`
	TTL             int    `json:"ttl" yaml:"ttl"`
	EvictionPolicy  string `json:"eviction-policy" yaml:"eviction-policy"`
}

type RedisCacheConfig struct {
	Nodes             []string `json:"nodes" yaml:"nodes"`
	Username          string   `json:"username" yaml:"username"`
	Password          string   `json:"password" yaml:"password"`
	ConnectionTimeout int      `json:"connection-timeout" yaml:"connection-timeout"`
	ResponseTimeout   int      `json:"response-timeout" yaml:"response-timeout"`
	Retries           int      `json:"retries" yaml:"retries"`
	MaxRetryWait      int      `json:"max-retry-wait" yaml:"max-retry-wait"`
	MinRetryWait      int      `json:"min-retry-wait" yaml:"min-retry-wait"`
	ReadFromReplica   bool     `json:"read-from-replica" yaml:"read-from-replica"`
	UseSSL            bool     `json:"use-ssl" yaml:"use-ssl"`
}

// ─── Storage ─────────────────────────────────────────────────

type StorageConfig struct {
	Type     string         `json:"type" yaml:"type"`
	SQLite   SQLiteConfig   `json:"sqlite" yaml:"sqlite"`
	Postgres PostgresConfig `json:"postgres" yaml:"postgres"`
}

type SQLiteConfig struct {
	Dir string `json:"dir" yaml:"dir"`
}

type PostgresConfig struct {
	Host           string `json:"host" yaml:"host"`
	Port           int    `json:"port" yaml:"port"`
	Database       string `json:"database" yaml:"database"`
	Schema         string `json:"schema" yaml:"schema"`
	Username       string `json:"username" yaml:"username"`
	Password       string `json:"password" yaml:"password"`
	MinConnections int    `json:"min-connections" yaml:"min-connections"`
	MaxConnections int    `json:"max-connections" yaml:"max-connections"`
	UseSSL         bool   `json:"use-ssl" yaml:"use-ssl"`
}

// ─── LLM / Orchestration ─────────────────────────────────────

type LLMConfig struct {
	Providers      map[string]LLMProviderDef `json:"providers" yaml:"providers"`
	RequestTimeout string                    `json:"request-timeout" yaml:"request-timeout"`
	RateLimit      map[string]int            `json:"rate-limit" yaml:"rate-limit"`
}

type OrchestrationConfig struct {
	MCPs                 []MCPDef     `json:"mcps" yaml:"mcps"`
	Agents               []AgentDef   `json:"agents" yaml:"agents"`
	AgentFlows           AgentFlowCfg `json:"agentflows" yaml:"agentflows"`
	MaxConcurrentFlows   int          `json:"max-concurrent-flows" yaml:"max-concurrent-flows"`
	FlowExecutionTimeout string       `json:"flow-execution-timeout" yaml:"flow-execution-timeout"`
	MaxNodeRetries       int          `json:"max-node-retries" yaml:"max-node-retries"`
}

type LLMProviderDef struct {
	Endpoint    string            `json:"endpoint" yaml:"endpoint"`
	Credentials map[string]string `json:"credentials" yaml:"credentials"`
	Proxy       string            `json:"proxy" yaml:"proxy"`
	Models      []ModelDef        `json:"models" yaml:"models"`
}

// ModalitiesConfig supports the nested YAML format:
//
//	modalities:
//	  input: [text]
//	  output: [text]
type ModalitiesConfig struct {
	Input  []string `json:"input" yaml:"input"`
	Output []string `json:"output" yaml:"output"`
}

type ModelDef struct {
	Name        string            `json:"name" yaml:"name"`
	Temperature float64           `json:"temperature" yaml:"temperature"`
	TopK        int               `json:"topk" yaml:"topk"`
	Modalities  *ModalitiesConfig `json:"modalities" yaml:"modalities"`
	Thinking    *ThinkingConfig   `json:"thinking" yaml:"thinking"`
}

type ThinkingConfig struct {
	Type         string `json:"type" yaml:"type"`
	BudgetTokens int    `json:"budget_tokens" yaml:"budget-tokens"`
}

type MCPDef struct {
	Name    string            `json:"name" yaml:"name"`
	Enabled bool              `json:"enabled" yaml:"enabled"`
	Type    string            `json:"type" yaml:"type"`
	Command []string          `json:"command" yaml:"command"`
	Args    []string          `json:"args" yaml:"args"`
	Env     map[string]string `json:"env" yaml:"env"`
}

type AgentDef struct {
	Name        string `json:"name" yaml:"name"`
	Model       string `json:"model" yaml:"model"`
	Soul        string `json:"soul" yaml:"soul"`
	Instruction string `json:"instruction" yaml:"instruction"`
}

type AgentFlowCfg struct {
	Static   StaticAgentFlowCfg   `json:"static" yaml:"static"`
	Standard StandardAgentFlowCfg `json:"standard" yaml:"standard"`
}

type StaticAgentFlowCfg struct {
	Enabled bool     `json:"enabled" yaml:"enabled"`
	Refresh string   `json:"refresh" yaml:"refresh"`
	Paths   []string `json:"paths" yaml:"paths"`
}

type StandardAgentFlowCfg struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// ─── AppConfig ───────────────────────────────────────────────

// AppConfig is the aggregate application configuration combining service config
// with loaded agentflow definitions.
type AppConfig struct {
	Service  ServiceConfig            `json:"service" yaml:"service"`
	Flows    []model.AgentFlowSpec    `json:"flows" yaml:"flows"`
	SubFlows map[string]model.AgentFlowSpec `json:"sub_flows,omitempty" yaml:"sub_flows,omitempty"`
}

// GetAgent returns the agent definition by name, or nil if not found.
func (c *AppConfig) GetAgent(name string) *AgentDef {
	for i := range c.Service.Orchestration.Agents {
		a := &c.Service.Orchestration.Agents[i]
		if a.Name == name {
			return a
		}
	}
	return nil
}

// GetMCP returns the MCP definition by name if enabled, or nil if not found.
func (c *AppConfig) GetMCP(name string) *MCPDef {
	for i := range c.Service.Orchestration.MCPs {
		m := &c.Service.Orchestration.MCPs[i]
		if m.Name == name && m.Enabled {
			return m
		}
	}
	return nil
}

// GetFlow returns the agentflow spec by ID, searching top-level flows first,
// then sub-flows.
func (c *AppConfig) GetFlow(id string) *model.AgentFlowSpec {
	for i := range c.Flows {
		if c.Flows[i].ID == id {
			return &c.Flows[i]
		}
	}
	if sw, ok := c.SubFlows[id]; ok {
		return &sw
	}
	return nil
}

// GetModel returns the first model name for the given provider.
func (c *AppConfig) GetModel(provider string) string {
	if p, ok := c.Service.LLM.Providers[provider]; ok {
		if len(p.Models) > 0 {
			return p.Models[0].Name
		}
	}
	return ""
}

// ─── Config file I/O ─────────────────────────────────────────

// Load reads the main service config YAML file.
func Load(path string) (*ServiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg ServiceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// LoadAgentFlows discovers and loads all L2 agentflow YAML files.
func LoadAgentFlows(cfg *ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	cfgDir := filepath.Dir(cfgPath)
	var flows []model.AgentFlowSpec
	subFlows := make(map[string]model.AgentFlowSpec)

	if cfg.Orchestration.AgentFlows.Static.Enabled {
		for _, p := range cfg.Orchestration.AgentFlows.Static.Paths {
			fullPath := filepath.Join(cfgDir, p)
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				entries, _ := os.ReadDir(fullPath)
				for _, e := range entries {
					if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
						loadAgentFlowFile(filepath.Join(fullPath, e.Name()), &flows, subFlows)
					}
				}
			} else {
				loadAgentFlowFile(fullPath, &flows, subFlows)
			}
		}
	}

	return flows, subFlows, nil
}

func loadAgentFlowFile(path string, flows *[]model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var spec model.AgentFlowSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return
	}
	if spec.ID == "" {
		return
	}
	if filepath.Base(filepath.Dir(path)) == "sub" {
		subFlows[spec.ID] = spec
	} else {
		*flows = append(*flows, spec)
	}
}

// ReloadAgentFlows re-reads agentflow YAML files (for hot reload).
func ReloadAgentFlows(cfg *ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	return LoadAgentFlows(cfg, cfgPath)
}

// BuildAppConfig combines service config with loaded flows.
func BuildAppConfig(cfg *ServiceConfig, flows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) *AppConfig {
	return &AppConfig{
		Service:  *cfg,
		Flows:    flows,
		SubFlows: subFlows,
	}
}
