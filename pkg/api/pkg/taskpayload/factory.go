package taskpayload

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// NewProvider builds the single explicitly configured TaskRun payload provider
// owned by the API Server.
func NewProvider(ctx context.Context, cfg config.ArtifactStorageConfig) (ITaskPayloadProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, fmt.Errorf("storage.artifacts.provider is required (expected default, s3, or gcs)")
	}
	if cfg.InlineMaxBytes < 0 {
		return nil, fmt.Errorf("storage.artifacts.inline_max_bytes must be non-negative")
	}
	if cfg.MaxPayloadBytes < 0 {
		return nil, fmt.Errorf("storage.artifacts.max_payload_bytes must be non-negative")
	}

	switch provider {
	case "default":
		return NewDefaultTaskPayloadProvider(cfg.MaxPayloadBytes), nil
	case "s3":
		return NewS3TaskPayloadProvider(ctx, cfg)
	case "gcs":
		return NewGCSTaskPayloadProvider(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported storage.artifacts.provider %q (expected default, s3, or gcs)", cfg.Provider)
	}
}

func newConfiguredObjectProvider(provider string, store objectStore, cfg config.ArtifactStorageConfig) (*objectTaskPayloadProvider, error) {
	putTimeout, err := parseTimeout("storage.artifacts.put_timeout", cfg.PutTimeout)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	getTimeout, err := parseTimeout("storage.artifacts.get_timeout", cfg.GetTimeout)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	result, err := newObjectTaskPayloadProvider(objectProviderOptions{
		Name: provider, Store: store, InlineMaxBytes: cfg.InlineMaxBytes,
		MaxPayloadBytes: cfg.MaxPayloadBytes, Compression: cfg.Compression,
		Prefix: cfg.Prefix, PutTimeout: putTimeout, GetTimeout: getTimeout,
		VerifyChecksum: cfg.VerifyChecksum,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return result, nil
}

func parseTimeout(name, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 30 * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration: %q", name, value)
	}
	return duration, nil
}

func configuredSecret(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return ""
	}
	return value
}
