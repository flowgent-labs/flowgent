package trace

import (
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// NewJaegerClientFromConfig centralizes API Server and all-in-one trace-query
// composition. A nil client means OTEL export may be enabled but no query
// backend has been configured.
func NewJaegerClientFromConfig(cfg config.OTELConfig) (*JaegerClient, error) {
	if cfg.QueryEndpoint == "" {
		return nil, nil
	}
	return NewJaegerClient(
		cfg.QueryEndpoint,
		time.Duration(cfg.QueryTimeout)*time.Millisecond,
		cfg.QueryLookback,
		cfg.QueryLimit,
	)
}
