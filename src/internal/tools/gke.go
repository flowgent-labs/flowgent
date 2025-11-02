package tools

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"time"
)

// GKEConfig GKE 配置
type GKEConfig struct {
	Cluster   string `json:"cluster"`
	Zone      string `json:"zone"`
	ProjectID string `json:"project_id"`
	Namespace string `json:"namespace"`
}

// DeployStatus 部署状态
type DeployStatus struct {
	Success    bool   `json:"success"`
	ServiceURL string `json:"service_url"`
	Message    string `json:"message"`
}

// GKETool GKE 部署验证工具
type GKETool struct {
	config GKEConfig
}

// NewGKETool 创建 GKE 工具
func NewGKETool(config GKEConfig) *GKETool {
	return &GKETool{config: config}
}

// WaitAndVerify 等待 CI 部署完成并验证
func (t *GKETool) WaitAndVerify(ctx context.Context, serviceName string) (*DeployStatus, error) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for deployment")
		case <-ticker.C:
			// 1. 检查 deployment rollout 状态
			err := t.checkRolloutStatus(ctx, serviceName)
			if err != nil {
				continue
			}

			// 2. 检查 Pod 状态
			ready, err := t.checkPodsReady(ctx, serviceName)
			if err != nil || !ready {
				continue
			}

			// 3. 获取服务 URL
			url, err := t.getServiceURL(ctx, serviceName)
			if err != nil {
				continue
			}

			// 4. 健康检查
			if t.healthCheck(ctx, url) {
				return &DeployStatus{
					Success:    true,
					ServiceURL: url,
					Message:    "Deployment successful and healthy",
				}, nil
			}
		}
	}
}

// checkRolloutStatus 检查 deployment rollout 状态
func (t *GKETool) checkRolloutStatus(ctx context.Context, serviceName string) error {
	cmd := exec.CommandContext(ctx, "kubectl",
		"rollout", "status",
		fmt.Sprintf("deployment/%s", serviceName),
		"-n", t.config.Namespace,
		"--timeout=30s",
		"--context", fmt.Sprintf("gke_%s_%s_%s", t.config.ProjectID, t.config.Zone, t.config.Cluster),
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("rollout status: %w (%s)", err, stderr.String())
	}
	return nil
}

// checkPodsReady 检查 Pod 是否就绪
func (t *GKETool) checkPodsReady(ctx context.Context, serviceName string) (bool, error) {
	cmd := exec.CommandContext(ctx, "kubectl",
		"get", "pods",
		"-l", fmt.Sprintf("app=%s", serviceName),
		"-n", t.config.Namespace,
		"-o", "jsonpath={.items[*].status.conditions[?(@.type=='Ready')].status}",
		"--context", fmt.Sprintf("gke_%s_%s_%s", t.config.ProjectID, t.config.Zone, t.config.Cluster),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return false, fmt.Errorf("get pods: %w (%s)", err, stderr.String())
	}

	output := stdout.String()
	if output == "" {
		return false, nil
	}

	// 检查所有 Pod 是否都 Ready (True)
	for _, status := range output {
		if status != 'T' {
			return false, nil
		}
	}
	return true, nil
}

// getServiceURL 获取服务访问 URL
func (t *GKETool) getServiceURL(ctx context.Context, serviceName string) (string, error) {
	// 获取 LoadBalancer IP
	ipCmd := exec.CommandContext(ctx, "kubectl",
		"get", "service", serviceName,
		"-n", t.config.Namespace,
		"-o", "jsonpath={.status.loadBalancer.ingress[0].ip}",
		"--context", fmt.Sprintf("gke_%s_%s_%s", t.config.ProjectID, t.config.Zone, t.config.Cluster),
	)

	var ipOut, ipErr bytes.Buffer
	ipCmd.Stdout = &ipOut
	ipCmd.Stderr = &ipErr

	if err := ipCmd.Run(); err != nil {
		// 尝试获取 hostname
		hostCmd := exec.CommandContext(ctx, "kubectl",
			"get", "service", serviceName,
			"-n", t.config.Namespace,
			"-o", "jsonpath={.status.loadBalancer.ingress[0].hostname}",
			"--context", fmt.Sprintf("gke_%s_%s_%s", t.config.ProjectID, t.config.Zone, t.config.Cluster),
		)
		var hostOut bytes.Buffer
		hostCmd.Stdout = &hostOut
		if err := hostCmd.Run(); err != nil {
			return "", fmt.Errorf("get IP/hostname: %w", err)
		}
		ip := hostOut.String()
		if ip == "" {
			return "", fmt.Errorf("no external IP or hostname")
		}
		ipOut.WriteString(ip)
	}

	ip := ipOut.String()
	if ip == "" {
		return "", fmt.Errorf("no external IP")
	}

	// 获取端口
	portCmd := exec.CommandContext(ctx, "kubectl",
		"get", "service", serviceName,
		"-n", t.config.Namespace,
		"-o", "jsonpath={.spec.ports[0].port}",
		"--context", fmt.Sprintf("gke_%s_%s_%s", t.config.ProjectID, t.config.Zone, t.config.Cluster),
	)

	var portOut bytes.Buffer
	portCmd.Stdout = &portOut

	if err := portCmd.Run(); err != nil {
		return "", fmt.Errorf("get port: %w", err)
	}

	port := portOut.String()
	if port == "" {
		port = "80"
	}

	return fmt.Sprintf("http://%s:%s", ip, port), nil
}

// healthCheck HTTP 健康检查
func (t *GKETool) healthCheck(ctx context.Context, url string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
