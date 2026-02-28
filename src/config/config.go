package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/flowgent-labs/flowgent/src/model"
	"github.com/flowgent-labs/flowgent/src/payments"
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
	Payments      *payments.PaymentsConfig `json:"payments" yaml:"payments"`
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
	Enabled bool          `json:"enabled" yaml:"enabled"`
	Host    string        `json:"host" yaml:"host"`
	Port    int           `json:"port" yaml:"port"`
	PProf   PProfConfig   `json:"pprof" yaml:"pprof"`
	OTEL    OTELConfig    `json:"otel" yaml:"otel"`
	Metrics MetricsConfig `json:"metrics" yaml:"metrics"`
}

type PProfConfig struct {
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	ServerBind string `json:"server-bind" yaml:"server-bind"`
}

type OTELConfig struct {
	Enabled    bool    `json:"enabled" yaml:"enabled"`
	Endpoint   string  `json:"endpoint" yaml:"endpoint"`
	Protocol   string  `json:"protocol" yaml:"protocol"`
	Timeout    int     `json:"timeout" yaml:"timeout"`
	SampleRate float64 `json:"sample_rate" yaml:"sample_rate"`
}

type MetricsConfig struct {
	Enabled              bool              `json:"enabled" yaml:"enabled"`
	Prometheus           bool              `json:"prometheus" yaml:"prometheus"`
	ExportInterval       time.Duration     `json:"export_interval" yaml:"export_interval"`
	HistogramBoundaries  MetricsBoundaries `json:"histogram_boundaries" yaml:"histogram_boundaries"`
	Labels               map[string]string `json:"labels" yaml:"labels"`
}

type MetricsBoundaries struct {
	Task  []float64 `json:"task" yaml:"task"`
	LLM   []float64 `json:"llm" yaml:"llm"`
	Queue []float64 `json:"queue" yaml:"queue"`
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
	MCPs                 []MCPDef    `json:"mcps" yaml:"mcps"`
	Agents               ResourceCfg `json:"agents" yaml:"agents"`
	Skills               ResourceCfg `json:"skills,omitempty" yaml:"skills,omitempty"`
	AgentFlows           ResourceCfg `json:"agentflows" yaml:"agentflows"`
	MaxConcurrentFlows   int         `json:"max-concurrent-flows" yaml:"max-concurrent-flows"`
	FlowExecutionTimeout string      `json:"flow-execution-timeout" yaml:"flow-execution-timeout"`
	MaxNodeRetries       int         `json:"max-node-retries" yaml:"max-node-retries"`
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
	Name         string         `json:"name" yaml:"name"`
	Model        string         `json:"model" yaml:"model"`
	Soul         string         `json:"soul" yaml:"soul"`
	Instruction  string         `json:"instruction" yaml:"instruction"`
	OutputSchema map[string]any `json:"output_schema,omitempty" yaml:"output_schema,omitempty"` // optional JSON Schema
	Temperature  *float64       `json:"temperature,omitempty" yaml:"temperature,omitempty"`     // override model default
	MaxTokens    int            `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`       // output length control
}

// ResourceCfg is dual-source config for a resource type (agents, skills, flows).
type ResourceCfg struct {
	Static   StaticResourceCfg `json:"static" yaml:"static"`
	Standard StandardAgentCfg  `json:"standard" yaml:"standard"`
}

// StaticResourceCfg is shared config for directory-based static resource loading.
type StaticResourceCfg struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	LoadDir string `json:"load-dir" yaml:"load-dir"`
	Refresh string `json:"refresh" yaml:"refresh"` // e.g. "30s", "1m"
}

// StandardAgentCfg enables DB-backed resource definitions (future Flowgent UI).
type StandardAgentCfg struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// ─── AppConfig ───────────────────────────────────────────────

// AppConfig is the aggregate application configuration combining service config
// with loaded agentflow definitions.
type AppConfig struct {
	Service  ServiceConfig                 `json:"service" yaml:"service"`
	Agents   []AgentDef                    `json:"agents,omitempty" yaml:"agents,omitempty"`
	Flows    []model.AgentFlowSpec         `json:"flows" yaml:"flows"`
	SubFlows map[string]model.AgentFlowSpec `json:"sub_flows,omitempty" yaml:"sub_flows,omitempty"`
}

// GetAgent returns the agent definition by name, or nil if not found.
func (c *AppConfig) GetAgent(name string) *AgentDef {
	for i := range c.Agents {
		a := &c.Agents[i]
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

// Load reads the main service config YAML file with env var overrides via viper.
// Environment variables prefixed with FLOWGENT_ take precedence over YAML values.
// Naming: FLOWGENT_SERVER_PORT overrides server.port, etc.
func Load(path string) (*ServiceConfig, error) {
	v := viper.New()

	// Config file
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Environment variable overrides — FLOWGENT_SERVER_PORT → server.port
	v.SetEnvPrefix("FLOWGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read YAML config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg ServiceConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// loadResourceDir loads all .yaml files from a directory into a slice of T.
func loadResourceDir[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var result []T
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var item T
		if err := yaml.Unmarshal(data, &item); err != nil {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

// LoadAgents loads agent definitions from the static directory.
func LoadAgents(cfg *ServiceConfig, cfgPath string) ([]AgentDef, error) {
	var agents []AgentDef
	if cfg.Orchestration.Agents.Static.Enabled {
		dir := filepath.Join(filepath.Dir(cfgPath), cfg.Orchestration.Agents.Static.LoadDir)
		return loadResourceDir[AgentDef](dir)
	}
	return agents, nil
}

// LoadAgentFlows discovers and loads all L2 agentflow YAML files from the static directory.
func LoadAgentFlows(cfg *ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	var flows []model.AgentFlowSpec
	subFlows := make(map[string]model.AgentFlowSpec)

	if cfg.Orchestration.AgentFlows.Static.Enabled {
		dir := filepath.Join(filepath.Dir(cfgPath), cfg.Orchestration.AgentFlows.Static.LoadDir)
		all, err := loadResourceDir[model.AgentFlowSpec](dir)
		if err != nil {
			return flows, subFlows, nil
		}
		for _, spec := range all {
			if spec.ID == "" {
				continue
			}
			flows = append(flows, spec)
		}
	}

	// Also load skills if configured
	if cfg.Orchestration.Skills.Static.Enabled {
		dir := filepath.Join(filepath.Dir(cfgPath), cfg.Orchestration.Skills.Static.LoadDir)
		all, err := loadResourceDir[model.AgentFlowSpec](dir)
		if err == nil {
			for _, spec := range all {
				if spec.ID == "" {
					continue
				}
				if spec.Kind == "skill" {
					flows = append(flows, spec)
				}
			}
		}
	}

	return flows, subFlows, nil
}

// ReloadAgentFlows re-reads agentflow YAML files (for hot reload).
func ReloadAgentFlows(cfg *ServiceConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	return LoadAgentFlows(cfg, cfgPath)
}

// BuildAppConfig combines service config with loaded agents and flows.
func BuildAppConfig(cfg *ServiceConfig, agents []AgentDef, flows []model.AgentFlowSpec, subFlows map[string]model.AgentFlowSpec) *AppConfig {
	return &AppConfig{
		Service:  *cfg,
		Agents:   agents,
		Flows:    flows,
		SubFlows: subFlows,
	}
}
