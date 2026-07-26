package config

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// ─── Top-level config ────────────────────────────────────────

// FlowgentConfig is the top-level runtime configuration for the flowgent engine.
// All resource definitions (agents, agentflows, MCPs, skills, LLM providers, channels)
// are now DB-backed and managed via the management console or REST API.
type FlowgentConfig struct {
	ServiceName         string                `json:"service_name" yaml:"service_name"`
	Server              ServerConfig          `json:"server" yaml:"server"`
	A2A                 A2AConfig             `json:"a2a" yaml:"a2a"`
	Mgmt                MgmtConfig            `json:"mgmt" yaml:"mgmt"`
	Logging             LoggingConfig         `json:"logging" yaml:"logging"`
	Auth                AuthConfig            `json:"auth" yaml:"auth"`
	Cache               CacheConfig           `json:"cache" yaml:"cache"`
	Storage             StorageConfig         `json:"storage" yaml:"storage"`
	Orchestration       OrchestrationConfig   `json:"orchestration" yaml:"orchestration"`
	Messager            MessagerConfig        `json:"messager" yaml:"messager"`
	Lock                LockConfig            `json:"lock" yaml:"lock"`
	Sandbox             SandboxConfig         `json:"sandbox" yaml:"sandbox"`
	Wallet              *WalletConfig         `json:"wallet" yaml:"wallet"`
	Notifier            NotifierConfig        `json:"notifier" yaml:"notifier"`
	Runtime             RuntimeConfig         `json:"runtime" yaml:"runtime"`
	ResolvedCredentials map[string]string     `json:"-" yaml:"-"`
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
	ContextPath     string `json:"context_path" yaml:"context_path"`
	ShutdownTimeout string `json:"shutdown_timeout" yaml:"shutdown_timeout"`
	MaxBodyBytes    int    `json:"max_body_bytes" yaml:"max_body_bytes"`
	ReadTimeout     string `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout    string `json:"write_timeout" yaml:"write_timeout"`
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
	ServerBind string `json:"server_bind" yaml:"server_bind"`
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
	JWTValidityAK  int              `json:"jwt_validity_ak" yaml:"jwt_validity_ak"`
	JWTValidityRK  int              `json:"jwt_validity_rk" yaml:"jwt_validity_rk"`
	JWTAlgorithm   string           `json:"jwt_algorithm" yaml:"jwt_algorithm"`
	JWTPrivateKey  string           `json:"jwt_private_key" yaml:"jwt_private_key"`
	JWTPublicKey   string           `json:"jwt_public_key" yaml:"jwt_public_key"`
	AnonymousPaths []string         `json:"anonymous_paths" yaml:"anonymous_paths"`
	OIDC           OIDCConfig       `json:"oidc" yaml:"oidc"`
	LDAP           LDAPConfig       `json:"ldap" yaml:"ldap"`
	GitHub         GitHubAuthConfig `json:"github" yaml:"github"`
}

type OIDCConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	ClientID     string `json:"client_id" yaml:"client_id"`
	ClientSecret string `json:"client_secret" yaml:"client_secret"`
	IssueURL     string `json:"issue_url" yaml:"issue_url"`
	RedirectURL  string `json:"redirect_url" yaml:"redirect_url"`
	Scope        string `json:"scope" yaml:"scope"`
}

// LDAPConfig configures enterprise LDAP/AD authentication.
//
// Field naming mirrors Spring's LdapContextSource + LdapTemplate patterns.
//
// # Connection (like Spring's LdapContextSource)
//
//	url:      ldaps://aa-lds-prod.us.mycompany:3269  (Global Catalog SSL)
//	base_dn:  DC=InfoDir,DC=Prod,DC=MyCompany
//	user_dn:  CN=GB-MyAPP-AD-OPS,OU=Alternate Accounts,OU=MyCompanyPeople,DC=…
//	password: "${LDAP_PASSWORD}"
//	referral: follow  (≡ LdapContextSource.setReferral("follow"))
//
// # User search (like Spring's EqualsFilter + SUBTREE_SCOPE + countLimit=1)
//
//	user_search_filter: (CN=%s)
//	username_attribute: CN
//
// # Attribute mapping (like Spring's CustomLdapContextMapper)
//
// Maps internal field names to LDAP attribute names configured per deployment.
//
//	user_attr_mapping:
//	  identifier:   "cn"
//	  display_name: "displayName"
//	  email:        "mail"
//	  groups:       "memberOf"
type LDAPConfig struct {
	Enabled              bool               `json:"enabled" yaml:"enabled"`
	URL                  string             `json:"url" yaml:"url"`                                       // ldap[s]://host:port
	BaseDN               string             `json:"base_dn" yaml:"base_dn"`                               // root base DN
	UserDN               string             `json:"user_dn" yaml:"user_dn"`                               // service account DN
	Password             string             `json:"password" yaml:"password"`                             // service account password
	Domains              []LDAPDomainConfig `json:"domains" yaml:"domains"`                               // AD multi-domain search
	RoleMapping          []LDAPRoleMapping  `json:"role_mapping" yaml:"role_mapping"`                     // AD group/domain → role
	UserSearchFilter     string             `json:"user_search_filter" yaml:"user_search_filter"`         // default: (cn=%s)
	UsernameAttribute    string             `json:"username_attribute" yaml:"username_attribute"`         // default: cn
	EmailAttribute       string             `json:"email_attribute" yaml:"email_attribute"`               // default: mail
	DisplayNameAttribute string             `json:"display_name_attribute" yaml:"display_name_attribute"` // default: cn
	GroupSearchBase      string             `json:"group_search_base" yaml:"group_search_base"`           // optional: for group→role resolution
	GroupSearchFilter    string             `json:"group_search_filter" yaml:"group_search_filter"`       // default: (member=%s)
	GroupNameAttribute   string             `json:"group_name_attribute" yaml:"group_name_attribute"`     // default: cn
	UserAttrMapping      map[string]string  `json:"user_attr_mapping" yaml:"user_attr_mapping"`           // internal name → LDAP attribute name
	Referral             string             `json:"referral" yaml:"referral"`                             // "follow" or "throw" (for AD multi-domain GC)
	InsecureSkipVerify   bool               `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}

// LDAPDomainConfig defines an AD domain to search for users.
type LDAPDomainConfig struct {
	BaseDN           string `json:"base_dn" yaml:"base_dn"`
	UserSearchFilter string `json:"user_search_filter" yaml:"user_search_filter"` // e.g. (sAMAccountName=%s)
}

// LDAPRoleMapping maps an AD group DN or domain base DN to a Flowgent built-in role.
type LDAPRoleMapping struct {
	Match string `json:"match" yaml:"match"` // AD group DN or domain base DN
	Role  string `json:"role" yaml:"role"`   // admin | operator | viewer
}

type GitHubAuthConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	ClientID     string `json:"client_id" yaml:"client_id"`
	ClientSecret string `json:"client_secret" yaml:"client_secret"`
	AuthURL      string `json:"auth_url" yaml:"auth_url"`
	TokenURL     string `json:"token_url" yaml:"token_url"`
	RedirectURL  string `json:"redirect_url" yaml:"redirect_url"`
	Scope        string `json:"scope" yaml:"scope"`
	UserInfoURL  string `json:"user_info_url" yaml:"user_info_url"`
}

// ─── Cache ───────────────────────────────────────────────────

type CacheConfig struct {
	Provider string            `json:"provider" yaml:"provider"`
	Memory   MemoryCacheConfig `json:"memory" yaml:"memory"`
	Redis    RedisCacheConfig  `json:"redis" yaml:"redis"`
}

type MemoryCacheConfig struct {
	InitialCapacity int    `json:"initial_capacity" yaml:"initial_capacity"`
	MaxCapacity     int    `json:"max_capacity" yaml:"max_capacity"`
	TTL             int    `json:"ttl" yaml:"ttl"`
	EvictionPolicy  string `json:"eviction_policy" yaml:"eviction_policy"`
}

type RedisCacheConfig struct {
	Nodes             []string `json:"nodes" yaml:"nodes"`
	Username          string   `json:"username" yaml:"username"`
	Password          string   `json:"password" yaml:"password"`
	ConnectionTimeout int      `json:"connection_timeout" yaml:"connection_timeout"`
	ResponseTimeout   int      `json:"response_timeout" yaml:"response_timeout"`
	Retries           int      `json:"retries" yaml:"retries"`
	MaxRetryWait      int      `json:"max_retry_wait" yaml:"max_retry_wait"`
	MinRetryWait      int      `json:"min_retry_wait" yaml:"min_retry_wait"`
	ReadFromReplica   bool     `json:"read_from_replica" yaml:"read_from_replica"`
	UseSSL            bool     `json:"use_ssl" yaml:"use_ssl"`
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
	MinConnections int    `json:"min_connections" yaml:"min_connections"`
	MaxConnections int    `json:"max_connections" yaml:"max_connections"`
	UseSSL         bool   `json:"use_ssl" yaml:"use_ssl"`
}

// ─── Orchestration ────────────────────────────────────────────

type OrchestrationConfig struct {
	MaxConcurrentFlows   int    `json:"max_concurrent_flows" yaml:"max_concurrent_flows"`
	FlowExecutionTimeout string `json:"flow_execution_timeout" yaml:"flow_execution_timeout"`
	MaxNodeRetries       int    `json:"max_node_retries" yaml:"max_node_retries"`
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

// MessagerConfig configures the message queue for inter-component communication.
type MessagerConfig struct {
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

// AgentInfo is the DB-backed agent definition type.
type AgentInfo = entities.AgentInfo

// McpInfo is the DB-backed MCP definition type.
type McpInfo = entities.McpInfo

// ─── Notifier ─────────────────────────────────────────────────

// NotifierConfig configures the notifier service (always-on daemon like apiserver).
// Channels are managed via DB CRUD API; this struct holds low-level tech settings.
type NotifierConfig struct {
	ScanInterval    string              `json:"scan_interval" yaml:"scan_interval"`
	CleanupInterval string              `json:"cleanup_interval" yaml:"cleanup_interval"`
	RouteTimeout    string              `json:"route_timeout" yaml:"route_timeout"`
	WebSocket       NotifierWSConfig    `json:"websocket" yaml:"websocket"`
	Telegram        NotifierTelegramCfg `json:"telegram" yaml:"telegram"`
	Email           NotifierEmailCfg    `json:"email" yaml:"email"`
}

// NotifierWSConfig holds WebSocket push notification settings for the notifier.
type NotifierWSConfig struct {
	PingInterval   string `json:"ping_interval" yaml:"ping_interval"`
	WriteTimeout   string `json:"write_timeout" yaml:"write_timeout"`
	MaxMessageSize int    `json:"max_message_size" yaml:"max_message_size"`
}

// NotifierTelegramCfg holds global Telegram Bot API defaults.
// Per-channel config (bot_token, chat_id) is stored in the DB.
type NotifierTelegramCfg struct {
	BaseURL string `json:"base_url" yaml:"base_url"`
}

// NotifierEmailCfg holds global SMTP defaults.
// Per-channel config (host, username, password, from) is stored in the DB.
type NotifierEmailCfg struct {
	SMTPPort int `json:"smtp_port" yaml:"smtp_port"`
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
	APIServerURL    string              `json:"api_server_url" yaml:"api_server_url"`
	Namespace       string              `json:"namespace" yaml:"namespace"`
	AgentFlowID     string              `json:"agent_flow_id" yaml:"agent_flow_id"`
	TMID            string              `json:"tm_id" yaml:"tm_id"`
	TMDeploy        string              `json:"tm_deploy" yaml:"tm_deploy"`
	TMSlots         int                 `json:"tm_slots" yaml:"tm_slots"`
	ControllerLabel string              `json:"controller_label" yaml:"controller_label"`
	JMImage         string              `json:"jm_image" yaml:"jm_image"`
	TMImage         string              `json:"tm_image" yaml:"tm_image"`
	JMConfigMap     string              `json:"jm_config_map" yaml:"jm_config_map"`
	PodIndex        int                 `json:"pod_index" yaml:"pod_index"`
	PodTotal        int                 `json:"pod_total" yaml:"pod_total"`
	Tenant          TenantConfig          `json:"tenant" yaml:"tenant"`
	CredentialPaths CredentialPathsConfig `json:"credential_paths" yaml:"credential_paths"`
}

// CredentialPathsConfig defines where credentials files are mounted in pods.
// Two-level hierarchy, flow overrides tenant:
//
//	/var/secret/flowgent/{tenant}/credentials          (tenant-level)
//	/var/secret/flowgent/{tenant}/{flow}/credentials    (flow-level)
//
// Only taskmanager, sandbox, and notifier load these at startup.
type CredentialPathsConfig struct {
	BasePath string `json:"base_path" yaml:"base_path"` // default: /var/secret/flowgent
}

// ─── Config file I/O ─────────────────────────────────────────

// Load reads the main service config YAML file with env var overrides via viper.
// Canonical config keys are snake_case, but relaxed binding also accepts kebab-case
// and camelCase (see matchConfigKey). Environment variables prefixed with FLOWGENT__
// (double underscore) use Spring Boot-style relaxed binding: __ separates nested config
// levels (FLOWGENT__RUNTIME__AGENT_FLOW_ID → runtime.agent_flow_id). FLOWGENT__ env vars
// take precedence over YAML file values.
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
	if err := v.Unmarshal(&cfg, func(c *mapstructure.DecoderConfig) {
		c.TagName = "yaml"
		// Separator- and case-insensitive key matching. Canonical config keys are
		// snake_case (see struct tags), but this Spring Boot-style relaxed binding
		// lets any equivalent spelling (snake_case, kebab-case, camelCase, or
		// viper-lowercased) bind to the same field.
		c.MatchName = matchConfigKey
	}); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Load CSI-mounted credentials (tenant + flow level)
	creds := loadCSICredentials(cfg.Runtime.CredentialPaths.BasePath)
	if len(creds) > 0 {
		cfg.ResolvedCredentials = creds
	}

	// Expand ${ENV_VAR} placeholders, falling back to resolved credentials
	expandEnvVarsWithCreds(reflect.ValueOf(&cfg).Elem(), cfg.ResolvedCredentials)
	return &cfg, nil
}

// applyFlowgentOverrides applies FLOWGENT__ environment variable overrides on top of
// the YAML config (env > YAML). Instead of blindly parsing env var names, it reflects
// the FlowgentConfig schema to derive the exact env-var → config-key mapping from the
// yaml tags. This keeps env names in sync with the config struct and — crucially —
// binds multi-word fields correctly (e.g. FLOWGENT__RUNTIME__JM_IMAGE maps to
// runtime.jm_image, which a naive __→. split would miss).
func applyFlowgentOverrides(v *viper.Viper) {
	envToKey := make(map[string]string)
	collectEnvKeys(reflect.TypeOf(FlowgentConfig{}), nil, envToKey)

	for _, e := range os.Environ() {
		name, val, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if key, found := envToKey[name]; found {
			v.Set(key, val)
		}
	}
}

// collectEnvKeys walks a config struct via reflection and records, for every leaf
// field, its canonical FLOWGENT__ env var name → dotted viper key path. Nested levels
// are joined with "__"; each key segment is converted to UPPER_SNAKE so that word
// boundaries stay readable (yaml `jm_image` ⇄ env FLOWGENT__RUNTIME__JM_IMAGE).
func collectEnvKeys(t reflect.Type, path []string, out map[string]string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		segs := append(append([]string(nil), path...), tag)

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			collectEnvKeys(ft, segs, out)
			continue
		}
		envSegs := make([]string, len(segs))
		for j, s := range segs {
			envSegs[j] = keyToEnvSegment(s)
		}
		out["FLOWGENT__"+strings.Join(envSegs, "__")] = strings.Join(segs, ".")
	}
}

// matchConfigKey compares a config key against a struct field name/tag, ignoring
// separators ('-', '_') and case. This unifies camelCase / kebab-case / snake_case.
func matchConfigKey(mapKey, fieldName string) bool {
	return normalizeConfigKey(mapKey) == normalizeConfigKey(fieldName)
}

func normalizeConfigKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '-' || r == '_' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// keyToEnvSegment converts a single config-key segment (in any supported style) to
// its UPPER_SNAKE env form. Canonical keys are snake_case ("jm_image" → "JM_IMAGE");
// kebab-case and camelCase are also handled so the env mapping stays correct even if
// a tag is written in a different style ("agentFlowId" → "AGENT_FLOW_ID"). Digits are
// treated as part of the surrounding word, not a boundary, so alphanumeric abbreviations
// stay intact ("a2a" → "A2A", "x402" → "X402") instead of splitting into "A_2_A"/"X_402".
func keyToEnvSegment(s string) string {
	// Already contains explicit separators (snake_case / kebab-case) — normalize them.
	if strings.ContainsAny(s, "-_") {
		return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
	}
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			prevLowerOrDigit := (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
			boundary := r >= 'A' && r <= 'Z'
			if boundary && prevLowerOrDigit {
				b.WriteRune('_')
			}
		}
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
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

// ── Wallet config types ─────────────────────────────────────

type WalletConfig struct {
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Policies    PoliciesConfig `json:"policies" yaml:"policies"`
	SecretStore SecretStoreCfg `json:"secret_store" yaml:"secret_store"`
	X402        X402Cfg        `json:"x402" yaml:"x402"`
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

func LoadAgents(cfg *FlowgentConfig, cfgPath string) ([]AgentInfo, error) { return nil, nil }
func LoadAgentFlows(cfg *FlowgentConfig, cfgPath string) ([]entities.FlowInfo, map[string]entities.FlowInfo, error) {
	return nil, make(map[string]entities.FlowInfo), nil
}
func ReloadAgentFlows(cfg *FlowgentConfig, cfgPath string) ([]entities.FlowInfo, map[string]entities.FlowInfo, error) {
	return nil, make(map[string]entities.FlowInfo), nil
}

// LogConfig prints key configuration details (masks sensitive fields).
func LogConfig(cfg *FlowgentConfig) {
	switch cfg.Storage.Type {
	case "POSTGRE":
		pg := cfg.Storage.Postgres
		slog.Info("Storage", "type", "PostgreSQL", "host", pg.Host, "port", pg.Port, "db", pg.Database, "schema", pg.Schema, "user", pg.Username, "pool_min", pg.MinConnections, "pool_max", pg.MaxConnections, "ssl", pg.UseSSL)
	default:
		sq := cfg.Storage.SQLite
		dir := sq.Dir
		if dir == "" {
			dir = "~/.flowgent/sqlite"
		}
		slog.Info("Storage", "type", "SQLite", "dir", dir)
	}

	slog.Info("Cache", "provider", cfg.Cache.Provider)
	slog.Info("REST API", "host", cfg.Server.Host, "port", cfg.Server.Port, "context", cfg.Server.ContextPath)
	if cfg.A2A.Enabled {
		slog.Info("A2A API", "host", cfg.A2A.Host, "port", cfg.A2A.Port)
	} else {
		slog.Info("A2A API disabled")
	}
	if cfg.Mgmt.Enabled {
		slog.Info("Management", "host", cfg.Mgmt.Host, "port", cfg.Mgmt.Port, "pprof", cfg.Mgmt.PProf.Enabled, "otel", cfg.Mgmt.OTEL.Enabled)
	}

	slog.Info("Engine", "max_concurrent", cfg.Orchestration.MaxConcurrentFlows, "timeout", cfg.Orchestration.FlowExecutionTimeout, "max_retries", cfg.Orchestration.MaxNodeRetries)
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
	if len(pattern) >= 3 && pattern[len(pattern)-3:] == "/**" {
		pfx := pattern[:len(pattern)-3]
		return len(path) >= len(pfx) && path[:len(pfx)] == pfx
	}
	return false
}
