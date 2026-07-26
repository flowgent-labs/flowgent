//go:build x402

package client

import (
	"log/slog"
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client/facilitator"
	"github.com/flowgent-labs/flowgent/core/pkg/client/policy"
	"github.com/flowgent-labs/flowgent/core/pkg/client/signclient"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// NewHttpClient creates the appropriate IFlowgentAPIClient based on config.
// When wallet.enabled is true, returns an X402PaymentHttpClient with
// policy evaluation, async MQTT signing, and facilitator integration.
// When payments are disabled, falls back to GenericHttpClient.
func NewHttpClient(cfg *config.FlowgentConfig, q messager.IMessager) model.IFlowgentAPIClient {
	if cfg == nil || cfg.Wallet == nil || !cfg.Wallet.Enabled {
		return NewGenericHttpClient(30 * time.Second)
	}

	payCfg := cfg.Wallet

	timeout := resolveTimeout(payCfg)

	// Policy engine
	eng := policy.NewEngine(&payCfg.Policies, nil)

	// Facilitator client
	facEndpoint := payCfg.X402.DefaultFacilitator
	fc := facilitator.New(facEndpoint, timeout)

	// Sign client — async MQTT when a messager is available (distributed or local)
	var sc model.SignClient
	if q != nil {
		var err error
		sc, err = signclient.NewMqttSignClient(q, cfg.Runtime.Tenant.DefaultTenant, "", "", 30*time.Second)
		if err != nil {
			slog.Warn("MQTT sign client unavailable, falling back to GenericHttpClient", "err", err)
			return NewGenericHttpClient(30 * time.Second)
		}
	} else {
		slog.Warn("no messager available, falling back to GenericHttpClient")
		return NewGenericHttpClient(30 * time.Second)
	}

	maxRetries := payCfg.X402.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	client := NewX402PaymentHttpClient(X402Config{
		HTTPTimeout: timeout,
		MaxRetries:  maxRetries,
	}, eng, fc, sc, nil)

	slog.Info("X402PaymentHttpClient created", "timeout", timeout, "facilitator", facEndpoint, "sign", "mqtt")

	return client
}

func resolveTimeout(payCfg *config.WalletConfig) time.Duration {
	if payCfg.X402.Timeout != "" {
		if d, err := time.ParseDuration(payCfg.X402.Timeout); err == nil {
			return d
		}
	}
	return 30 * time.Second
}
