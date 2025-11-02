package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config 机器人配置
type Config struct {
	// 配置的 Repositories
	Repos []RepoConfig `json:"repos"`

	// MCP 服务器配置
	MCP MCPConfig `json:"mcp"`

	// GitHub 配置
	GitHub GitHubConfig `json:"github"`

	// GKE 配置
	GKE GKEConfig `json:"gke"`

	// 测试配置
	Test TestConfig `json:"test"`

	// 扫描间隔 (小时)
	ScanIntervalHours int `json:"scan_interval_hours"`
}

// RepoConfig 单个 Repository 配置
type RepoConfig struct {
	// 完整名称 owner/name
	FullName string `json:"full_name"`

	// Dev 分支名
	DevBranch string `json:"dev_branch"`

	// 服务名称 (用于部署验证)
	ServiceName string `json:"service_name"`

	// 扫描类型列表
	ScanTypes []string `json:"scan_types"` // sast, dast, cont, foss, sonar
}

// MCPConfig MCP 服务器配置
type MCPConfig struct {
	// 扫描集成服务器
	ScannerServer string `json:"scanner_server"`

	// 报告解析服务器
	ParserServer string `json:"parser_server"`

	// 修复方案服务器
	FixerServer string `json:"fixer_server"`

	// Git 操作服务器 (GitHub/GitLab provider)
	GitServer string `json:"git_server"`

	// 测试执行服务器 (Maven/Gradle/npm etc.)
	TestServer string `json:"test_server"`
}

// GitHubConfig GitHub 配置
type GitHubConfig struct {
	// Enterprise Base URL
	BaseURL string `json:"base_url"`

	// Token Secret Name (从 Secret Manager 获取)
	TokenSecretName string `json:"token_secret_name"`

	// 默认目标分支
	DefaultBranch string `json:"default_branch"`
}

// GKEConfig GKE 配置
type GKEConfig struct {
	// 集群名称
	Cluster string `json:"cluster"`

	// Zone
	Zone string `json:"zone"`

	// Project ID
	ProjectID string `json:"project_id"`

	// Namespace
	Namespace string `json:"namespace"`
}

// TestConfig 测试配置
type TestConfig struct {
	// 工作目录
	Workspace string `json:"workspace"`

	// Cucumber Profile
	CucumberProfile string `json:"cucumber_profile"`
}

// Load 从环境变量和配置文件加载配置
func Load() (*Config, error) {
	cfg := &Config{
		ScanIntervalHours: 1,
		Test: TestConfig{
			Workspace:       "/workspace",
			CucumberProfile: "integration",
		},
	}

	// 从配置文件加载 (如果存在)
	configPath := os.Getenv("SECURITY_BOT_CONFIG")
	if configPath == "" {
		configPath = os.Getenv("CYBERBOT_CONFIG")
	}
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config file: %w", err)
		}
	}

	// 环境变量覆盖
	if v := os.Getenv("GITHUB_BASE_URL"); v != "" {
		cfg.GitHub.BaseURL = v
	}

	// 验证必要配置
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	if len(c.Repos) == 0 {
		return fmt.Errorf("at least one repo must be configured")
	}
	for i, repo := range c.Repos {
		if repo.FullName == "" {
			return fmt.Errorf("repos[%d]: full_name is required", i)
		}
		if repo.DevBranch == "" {
			return fmt.Errorf("repos[%d]: dev_branch is required", i)
		}
		if repo.ServiceName == "" {
			return fmt.Errorf("repos[%d]: service_name is required", i)
		}
	}
	if c.GitHub.BaseURL == "" {
		return fmt.Errorf("github base_url is required")
	}
	if c.GKE.Cluster == "" {
		return fmt.Errorf("gke cluster is required")
	}
	if c.ScanIntervalHours <= 0 {
		return fmt.Errorf("scan_interval_hours must be positive")
	}
	return nil
}

// Timeout 返回扫描超时时间
func (c *Config) Timeout() time.Duration {
	return time.Duration(c.ScanIntervalHours) * time.Hour
}
