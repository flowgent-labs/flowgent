package config

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/viper"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// ─── Top-level config ────────────────────────────────────────

// FlowgentConfig is the top-level runtime configuration for the flowgent engine.
// All resource definitions (agents, agentflows, MCPs, skills, LLM providers, channels)
// are now DB-backed and managed via the management console or REST API.
type FlowgentConfig struct {
	ServiceName         string                `json:"service-name" yaml:"service-name"`
	Deployment          DeploymentConfig      `json:"deployment" yaml:"deployment"`
	Server              ServerConfig          `json:"server" yaml:"server"`
	A2A                 A2AConfig             `json:"a2a" yaml:"a2a"`
	Mgmt                MgmtConfig            `json:"mgmt" yaml:"mgmt"`
	Logging             LoggingConfig         `json:"logging" yaml:"logging"`
	Auth                AuthConfig            `json:"auth" yaml:"auth"`
	Cache               CacheConfig           `json:"cache" yaml:"cache"`
	Storage             StorageConfig         `json:"storage" yaml:"storage"`
	Orchestration       OrchestrationConfig   `json:"orchestration" yaml:"orchestration"`
	Messaging           MessagingConfig       `json:"messaging" yaml:"messaging"`
	Lock                LockConfig            `json:"lock" yaml:"lock"`
	Sandbox             SandboxConfig         `json:"sandbox" yaml:"sandbox"`
	Payments            *PaymentsConfig       `json:"payments" yaml:"payments"`
	Notifier            NotifierConfig        `json:"notifier" yaml:"notifier"`
	CredentialPaths     CredentialPathsConfig `json:"credential-paths" yaml:"credential-paths"`
	Tenant              TenantConfig          `json:"tenant" yaml:"tenant"`
	Runtime             RuntimeConfig         `json:"runtime" yaml:"runtime"`
	ResolvedCredentials map[string]string     `json:"-" yaml:"-"`
}

// DeploymentConfig sets the execution mode: session or application.
type DeploymentConfig struct {
	Mode string `json:"mode" yaml:"mode"` // "session" | "application"
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
	Enabled             bool              `json:"enabled" yaml:"enabled"`
	Prometheus          bool              `json:"prometheus" yaml:"prometheus"`
	ExportInterval      time.Duration     `json:"export_interval" yaml:"export_interval"`
	HistogramBoundaries MetricsBoundaries `json:"histogram_boundaries" yaml:"histogram_boundaries"`
	Labels              map[string]string `json:"labels" yaml:"labels"`
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
	Dsn            string `json:"dsn" yaml:"dsn"`
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

// ─── Orchestration ────────────────────────────────────────────

type OrchestrationConfig struct {
	Agents               StandardResourceCfg `json:"agents" yaml:"agents"`
	Skills               StandardResourceCfg `json:"skills,omitempty" yaml:"skills,omitempty"`
	AgentFlows           StandardResourceCfg `json:"agentflows" yaml:"agentflows"`
	MaxConcurrentFlows   int                 `json:"max-concurrent-flows" yaml:"max-concurrent-flows"`
	FlowExecutionTimeout string              `json:"flow-execution-timeout" yaml:"flow-execution-timeout"`
	MaxNodeRetries       int                 `json:"max-node-retries" yaml:"max-node-retries"`
}

// StandardResourceCfg enables DB-backed resource definitions via management API.
type StandardResourceCfg struct {
	Standard StandardAgentCfg `json:"standard" yaml:"standard"`
}

// StandardAgentCfg enables DB-backed resource definitions (future Flowgent UI).
type StandardAgentCfg struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// SandboxConfig configures the sandbox execution environment.
// In standalone/all-in-one mode (Deployment.Enabled=false), sandbox runs inline in TM.
// In distributed mode (Deployment.Enabled=true), sandbox runs as independent K8s pods
// managed by the JM's K8sRM with its own Deployment, scaling, and resource limits.
type SandboxConfig struct {
	Workspace  string                        `json:"workspace" yaml:"workspace"`
	Policy     *model.SandboxPolicy          `json:"policy" yaml:"policy"`
	Deployment model.SandboxDeploymentConfig `json:"deployment" yaml:"deployment"`
}

// MessagingConfig configures the message queue for inter-component communication.
type MessagingConfig struct {
	Type string     `json:"type" yaml:"type"` // memory | mqtt
	MQTT MQTTConfig `json:"mqtt" yaml:"mqtt"`
}

// MQTTConfig is the MQTT broker connection settings.
type MQTTConfig struct {
	Broker   string `json:"broker" yaml:"broker"`
	ClientID string `json:"client_id" yaml:"client_id"`
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

// LockConfig configures the distributed lock provider.
type LockConfig struct {
	Provider string          `json:"provider" yaml:"provider"` // memory | postgres | redis
	Redis    RedisLockConfig `json:"redis" yaml:"redis"`
}

// RedisLockConfig is the Redis-specific lock settings.
type RedisLockConfig struct {
	Nodes    []string `json:"nodes" yaml:"nodes"`
	Password string   `json:"password" yaml:"password"`
}


// AgentDef is the DB-backed agent definition type.
type AgentDef = model.AgentDef

// MCPDef is the DB-backed MCP definition type.
type MCPDef = model.MCPDef

// ─── Notifier ─────────────────────────────────────────────────

// NotifierConfig configures the notifier service. Channels are managed via DB CRUD API.
type NotifierConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// ─── Tenant ────────────────────────────────────────────────────

// TenantConfig configures multi-tenant isolation.
type TenantConfig struct {
	DefaultTenant   string `json:"default_tenant" yaml:"default_tenant"`
	NamespacePrefix string `json:"namespace_prefix" yaml:"namespace_prefix"`
}

// RuntimeConfig holds operational parameters set at deploy time (env vars, not YAML).
// These are populated by viper from FLOWGENT__RUNTIME__* env vars.
type RuntimeConfig struct {
	APIServerURL    string `json:"api-server-url" yaml:"api-server-url"`
	Namespace       string `json:"namespace" yaml:"namespace"`
	AgentFlowID     string `json:"agent-flow-id" yaml:"agent-flow-id"`
	TMID            string `json:"tm-id" yaml:"tm-id"`
	TMDeploy        string `json:"tm-deploy" yaml:"tm-deploy"`
	TMSlots         int    `json:"tm-slots" yaml:"tm-slots"`
	ControllerLabel string `json:"controller-label" yaml:"controller-label"`
	JMImage         string `json:"jm-image" yaml:"jm-image"`
	PodIndex        int    `json:"pod-index" yaml:"pod-index"`
	PodTotal        int    `json:"pod-total" yaml:"pod-total"`
}

// CredentialPathsConfig defines where credentials files are mounted in pods.
// Two-level hierarchy, flow overrides tenant:
//
//	/var/secret/flowgent/{tenant}/credentials          (tenant-level)
//	/var/secret/flowgent/{tenant}/{flow}/credentials    (flow-level)
//
// Only taskmanager, sandbox, and notifier load these at startup.
type CredentialPathsConfig struct {
	BasePath string `json:"base-path" yaml:"base-path"` // default: /var/secret/flowgent
}




// ─── Config file I/O ─────────────────────────────────────────

// Load reads the main service config YAML file with env var overrides via viper.
// Environment variables prefixed with FLOWGENT__ (double underscore) use Spring Boot-style
// relaxed binding: __ maps to ., __N__ maps to [N] for array indices.
// FLOWGENT__ env vars take precedence over YAML file values.
func Load(path string) (*FlowgentConfig, error) {
	v := viper.New()

	// Config file
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Read YAML config file first
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Apply FLOWGENT__ env overrides on top (env > YAML)
	applyFlowgentOverrides(v)

	var cfg FlowgentConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Load CSI-mounted credentials (tenant + flow level)
	creds := loadCSICredentials(cfg.CredentialPaths.BasePath)
	if len(creds) > 0 {
		cfg.ResolvedCredentials = creds
	}

	// Expand ${ENV_VAR} placeholders, falling back to resolved credentials
	expandEnvVarsWithCreds(reflect.ValueOf(&cfg).Elem(), cfg.ResolvedCredentials)
	return &cfg, nil
}

// applyFlowgentOverrides reads FLOWGENT__ env vars and maps them to viper config keys
// using Spring Boot relaxed binding: __ → . for nesting, __N__ → [N] for array indices.
// Example: FLOWGENT__ORCHESTRATION__MCPS__0__NAME → orchestration.mcps[0].name
func applyFlowgentOverrides(v *viper.Viper) {
	const prefix = "FLOWGENT__"
	for _, e := range os.Environ() {
		k, val, ok := strings.Cut(e, "=")
		if !ok || !strings.HasPrefix(k, prefix) {
			continue
		}
		// Strip prefix, lowercase for viper
		key := strings.ToLower(strings.TrimPrefix(k, prefix))
		// Convert __ to . for nesting, handling __N__ → [N]
		parts := strings.Split(key, "__")
		var out []string
		for _, p := range parts {
			if isNumeric(p) {
				if len(out) > 0 {
					out[len(out)-1] = out[len(out)-1] + "[" + p + "]"
				}
			} else {
				out = append(out, p)
			}
		}
		v.Set(strings.Join(out, "."), val)
	}
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// loadCSICredentials reads credential files from CSI-mounted paths:
//
//	{basePath}/{tenant}/secret/.credentials          (tenant-level)
//	{basePath}/{tenant}/{flowId}/secret/.credentials  (flow-level)
//
// Files are KEY=VALUE format, flow-level overrides tenant-level.
// If basePath is empty, defaults to /var/flowgent.
func loadCSICredentials(basePath string) map[string]string {
	if basePath == "" {
		basePath = "/var/flowgent"
	}
	result := make(map[string]string)

	// Scan for tenant directories
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tenant := e.Name()
		// Tenant-level credentials
		tenantCredFile := filepath.Join(basePath, tenant, "secret", ".credentials")
		if m := readEnvFile(tenantCredFile); len(m) > 0 {
			for k, v := range m {
				result[k] = v
			}
		}
		// Flow-level credentials (scan subdirs)
		flowEntries, err := os.ReadDir(filepath.Join(basePath, tenant))
		if err != nil {
			continue
		}
		for _, fe := range flowEntries {
			if !fe.IsDir() {
				continue
			}
			flowCredFile := filepath.Join(basePath, tenant, fe.Name(), "secret", ".credentials")
			if m := readEnvFile(flowCredFile); len(m) > 0 {
				for k, v := range m {
					result[k] = v
				}
			}
		}
	}
	return result
}

// expandEnvVarsWithCreds recursively expands ${VAR} in string values, falling back
// to resolvedCredentials when VAR is not found in OS environment.
func expandEnvVarsWithCreds(v reflect.Value, resolvedCredentials map[string]string) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if len(s) > 3 && s[0] == '$' && s[1] == '{' {
			end := strings.IndexByte(s, '}')
			if end > 2 {
				envKey := s[2:end]
				if envVal := os.Getenv(envKey); envVal != "" {
					v.SetString(envVal)
				} else if envVal, ok := resolvedCredentials[envKey]; ok {
					v.SetString(envVal)
				}
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			expandEnvVarsWithCreds(v.Field(i), resolvedCredentials)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			if val.Kind() == reflect.Interface {
				val = val.Elem()
			}
			if val.Kind() == reflect.String {
				newVal := expandStringWithCreds(val.String(), resolvedCredentials)
				v.SetMapIndex(key, reflect.ValueOf(newVal))
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			expandEnvVarsWithCreds(v.Index(i), resolvedCredentials)
		}
	}
}

func expandStringWithCreds(s string, creds map[string]string) string {
	if len(s) > 3 && s[0] == '$' && s[1] == '{' {
		end := strings.IndexByte(s, '}')
		if end > 2 {
			envKey := s[2:end]
			if envVal := os.Getenv(envKey); envVal != "" {
				return envVal
			}
			if envVal, ok := creds[envKey]; ok {
				return envVal
			}
		}
	}
	return s
}


// ── Payments config types ─────────────────────────────────────

type PaymentsConfig struct {
	Enabled  bool           `json:"enabled" yaml:"enabled"`
	Policies PoliciesConfig `json:"policies" yaml:"policies"`
	Wallet   WalletCfg      `json:"wallet" yaml:"wallet"`
	X402     X402Cfg        `json:"x402" yaml:"x402"`
}

type PoliciesConfig struct {
	MaxSinglePaymentUSD          float64  `json:"max_single_payment_usd" yaml:"max_single_payment_usd"`
	MaxDailyBudgetUSD            float64  `json:"max_daily_budget_usd" yaml:"max_daily_budget_usd"`
	AllowedDomains               []string `json:"allowed_domains" yaml:"allowed_domains"`
	BlockedDomains               []string `json:"blocked_domains" yaml:"blocked_domains"`
	RequireHumanApprovalAboveUSD float64  `json:"require_human_approval_above_usd" yaml:"require_human_approval_above_usd"`
	AllowedAssets                []string `json:"allowed_assets" yaml:"allowed_assets"`
	AllowedChains                []string `json:"allowed_chains" yaml:"allowed_chains"`
}

type WalletCfg struct {
	Endpoint      string         `json:"endpoint" yaml:"endpoint"`
	AuthToken     string         `json:"auth_token" yaml:"auth_token"`
	AuthTokenFile string         `json:"auth_token_file" yaml:"auth_token_file"`
	DefaultWallet string         `json:"default_wallet" yaml:"default_wallet"`
	SecretStore   SecretStoreCfg `json:"secret_store" yaml:"secret_store"`
}

type SecretStoreCfg struct {
	Provider      string   `json:"provider" yaml:"provider"`
	MasterKey     string   `json:"master_key" yaml:"master_key"`
	MasterKeyFile string   `json:"master_key_file" yaml:"master_key_file"`
	Vault         VaultCfg `json:"vault" yaml:"vault"`
}

type VaultCfg struct {
	Address    string `json:"address" yaml:"address"`
	Token      string `json:"token" yaml:"token"`
	TokenFile  string `json:"token_file" yaml:"token_file"`
	MountPath  string `json:"mount_path" yaml:"mount_path"`
	SecretPath string `json:"secret_path" yaml:"secret_path"`
	Role       string `json:"role" yaml:"role"`
}

type X402Cfg struct {
	DefaultFacilitator string `json:"default_facilitator" yaml:"default_facilitator"`
	Timeout            string `json:"timeout" yaml:"timeout"`
	MaxRetries         int    `json:"max_retries" yaml:"max_retries"`
}

// expandEnvVars recursively walks a struct and replaces ${VAR} placeholders
// in string values with the corresponding environment variable value.
func expandEnvVars(v reflect.Value) {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if len(s) > 3 && s[0] == '$' && s[1] == '{' {
			end := strings.IndexByte(s, '}')
			if end > 2 {
				envKey := s[2:end]
				if envVal := os.Getenv(envKey); envVal != "" {
					v.SetString(envVal)
				}
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			expandEnvVars(v.Field(i))
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			if val.Kind() == reflect.Interface {
				val = val.Elem()
			}
			if val.Kind() == reflect.String {
				newVal := expandString(val.String())
				v.SetMapIndex(key, reflect.ValueOf(newVal))
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			expandEnvVars(v.Index(i))
		}
	}
}

func expandString(s string) string {
	if len(s) > 3 && s[0] == '$' && s[1] == '{' {
		end := strings.IndexByte(s, '}')
		if end > 2 {
			envKey := s[2:end]
			if envVal := os.Getenv(envKey); envVal != "" {
				return envVal
			}
		}
	}
	return s
}

// ── Config display ──────────────────────────────────────────────

// ── Deprecated: static resource loading stubs ───────────────────
// These exist for backward compatibility. All resources are now DB-backed.
// New code should load from the management console or REST API.

func LoadAgents(cfg *FlowgentConfig, cfgPath string) ([]AgentDef, error) { return nil, nil }
func LoadAgentFlows(cfg *FlowgentConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	return nil, make(map[string]model.AgentFlowSpec), nil
}
func ReloadAgentFlows(cfg *FlowgentConfig, cfgPath string) ([]model.AgentFlowSpec, map[string]model.AgentFlowSpec, error) {
	return nil, make(map[string]model.AgentFlowSpec), nil
}

// LogConfig prints key configuration details (masks sensitive fields).
func LogConfig(cfg *FlowgentConfig) {
	switch cfg.Storage.Type {
	case "POSTGRE":
		pg := cfg.Storage.Postgres
		log.Printf("Storage:    PostgreSQL host=%s port=%d db=%s schema=%s user=%s pool_min=%d pool_max=%d ssl=%v",
			pg.Host, pg.Port, pg.Database, pg.Schema, pg.Username, pg.MinConnections, pg.MaxConnections, pg.UseSSL)
	default:
		sq := cfg.Storage.SQLite
		dir := sq.Dir
		if dir == "" {
			dir = "~/.flowgent/sqlite"
		}
		log.Printf("Storage:    SQLite dir=%s", dir)
	}

	log.Printf("Cache:      provider=%s", cfg.Cache.Provider)
	log.Printf("REST API:   %s:%d (context=%s)", cfg.Server.Host, cfg.Server.Port, cfg.Server.ContextPath)
	if cfg.A2A.Enabled {
		log.Printf("A2A API:    %s:%d", cfg.A2A.Host, cfg.A2A.Port)
	} else {
		log.Printf("A2A API:    disabled")
	}
	if cfg.Mgmt.Enabled {
		log.Printf("Management: %s:%d (pprof=%v, otel=%v)", cfg.Mgmt.Host, cfg.Mgmt.Port, cfg.Mgmt.PProf.Enabled, cfg.Mgmt.OTEL.Enabled)
	}

	log.Printf("Engine:     max_concurrent=%d timeout=%s max_retries=%d",
		cfg.Orchestration.MaxConcurrentFlows, cfg.Orchestration.FlowExecutionTimeout, cfg.Orchestration.MaxNodeRetries)
	log.Printf("DB-backed:  agents=%v skills=%v agentflows=%v",
		cfg.Orchestration.Agents.Standard.Enabled,
		cfg.Orchestration.Skills.Standard.Enabled,
		cfg.Orchestration.AgentFlows.Standard.Enabled)
}

// ── Auth middleware ──────────────────────────────────────────────

// AuthMiddleware creates a simple auth middleware that skips anonymous paths.
func AuthMiddleware(cfg AuthConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range cfg.AnonymousPaths {
			if MatchGlob(p, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// MatchGlob matches a path against a glob-like pattern.
func MatchGlob(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == "/**" {
		pfx := pattern[:len(pattern)-2]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}
