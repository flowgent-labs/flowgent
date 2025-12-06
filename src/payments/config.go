package payments

// PaymentsConfig is the top-level configuration for the economic runtime.
// When Enabled is false, all payment features are no-ops and Flowgent
// operates as a standard orchestration engine.
type PaymentsConfig struct {
	Enabled  bool           `json:"enabled" yaml:"enabled"`
	Policies PoliciesConfig `json:"policies" yaml:"policies"`
	Wallet   WalletConfig   `json:"wallet" yaml:"wallet"`
	X402     X402Config     `json:"x402" yaml:"x402"`
}

// PoliciesConfig defines spending limits and approval thresholds.
type PoliciesConfig struct {
	MaxSinglePaymentUSD           float64  `json:"max_single_payment_usd" yaml:"max_single_payment_usd"`
	MaxDailyBudgetUSD             float64  `json:"max_daily_budget_usd" yaml:"max_daily_budget_usd"`
	AllowedDomains                []string `json:"allowed_domains" yaml:"allowed_domains"`
	BlockedDomains                []string `json:"blocked_domains" yaml:"blocked_domains"`
	RequireHumanApprovalAboveUSD  float64  `json:"require_human_approval_above_usd" yaml:"require_human_approval_above_usd"`
	AllowedAssets                 []string `json:"allowed_assets" yaml:"allowed_assets"`
	AllowedChains                 []string `json:"allowed_chains" yaml:"allowed_chains"`
}

// WalletConfig configures the wallet service connection.
type WalletConfig struct {
	Endpoint   string            `json:"endpoint" yaml:"endpoint"`
	AuthToken  string            `json:"auth_token" yaml:"auth_token"`
	AuthTokenFile string         `json:"auth_token_file" yaml:"auth_token_file"`
	DefaultWallet string         `json:"default_wallet" yaml:"default_wallet"`
	SecretStore SecretStoreConfig `json:"secret_store" yaml:"secret_store"`
}

// SecretStoreConfig configures the secret storage backend.
type SecretStoreConfig struct {
	Provider    string            `json:"provider" yaml:"provider"` // "default" or "vault"
	MasterKey   string            `json:"master_key" yaml:"master_key"`
	MasterKeyFile string          `json:"master_key_file" yaml:"master_key_file"`
	Vault       VaultConfig       `json:"vault" yaml:"vault"`
}

// VaultConfig configures the Hashicorp Vault provider.
type VaultConfig struct {
	Address       string `json:"address" yaml:"address"`
	Token         string `json:"token" yaml:"token"`
	TokenFile     string `json:"token_file" yaml:"token_file"`
	MountPath     string `json:"mount_path" yaml:"mount_path"`
	SecretPath    string `json:"secret_path" yaml:"secret_path"`
	Role          string `json:"role" yaml:"role"`
}

// X402Config configures the x402 protocol behavior.
type X402Config struct {
	DefaultFacilitator string `json:"default_facilitator" yaml:"default_facilitator"`
	Timeout            string `json:"timeout" yaml:"timeout"`
	MaxRetries         int    `json:"max_retries" yaml:"max_retries"`
}
