//go:build x402

package client

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	x402 "github.com/x402-foundation/x402/go"
	x402http "github.com/x402-foundation/x402/go/http"
	exactevm "github.com/x402-foundation/x402/go/mechanisms/evm/exact/client"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client/policy"
	"github.com/flowgent-labs/flowgent/core/pkg/client/signclient"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// NewHttpClient creates the appropriate IFlowgentAPIClient based on config.
// When wallet.enabled is true, returns an X402PaymentHttpClient with
// policy evaluation and external Wallet signing. The resource server owns
// facilitator verification and settlement.
// When payments are disabled, falls back to GenericHttpClient.
func NewHttpClient(cfg *config.FlowgentConfig, _ messager.IMessager) model.IFlowgentAPIClient {
	if cfg == nil || cfg.Wallet == nil || !cfg.Wallet.Enabled {
		return NewGenericHttpClient(30 * time.Second)
	}

	payCfg := cfg.Wallet

	timeout := resolveTimeout(payCfg)

	// Policy engine
	eng := policy.NewEngine(&payCfg.Policies, nil)

	if payCfg.KeyID == "" || payCfg.PublicAddress == "" {
		slog.Warn("wallet client identity is incomplete, falling back to GenericHttpClient")
		return NewGenericHttpClient(30 * time.Second)
	}

	signTimeout := resolveSignTimeout(payCfg)
	clientIDPrefix := payCfg.ClientIDPrefix
	if clientIDPrefix == "" {
		clientIDPrefix = "flowgent-"
	}
	clientID := clientIDPrefix + uuid.NewString()
	if err := signclient.ValidateIdentity(clientID, payCfg.KeyID, payCfg.PublicAddress); err != nil {
		slog.Warn("external wallet client identity is invalid, falling back to GenericHttpClient", "err", err)
		return NewGenericHttpClient(30 * time.Second)
	}
	var transport signclient.Transport
	var transportErr error
	switch payCfg.Transport {
	case "local":
		socket := payCfg.LocalSocket
		if socket == "" {
			socket = "/run/wallet/wallet.sock"
		}
		transport, transportErr = signclient.NewLocalTransport(socket, signTimeout)
	case "", "mqtt":
		transport, transportErr = signclient.NewMQTTTransport(signclient.MQTTConfig{
			Broker:              cfg.Messager.MQTT.Broker,
			ClientID:            clientID,
			Username:            cfg.Messager.MQTT.Username,
			Password:            cfg.Messager.MQTT.Password,
			RequestTopicPrefix:  signclient.DefaultRequestTopicPrefix,
			ResponseTopicPrefix: signclient.DefaultResponseTopicPrefix,
			Timeout:             signTimeout,
		})
	default:
		transportErr = fmt.Errorf("unsupported wallet transport %q", payCfg.Transport)
	}
	if transportErr != nil {
		slog.Warn("external wallet transport unavailable, falling back to GenericHttpClient", "err", transportErr)
		return NewGenericHttpClient(30 * time.Second)
	}
	digestClient, err := signclient.NewDigestClient(transport, clientID, payCfg.KeyID, payCfg.PublicAddress, signTimeout)
	if err != nil {
		slog.Warn("external wallet client is invalid, falling back to GenericHttpClient", "err", err)
		return NewGenericHttpClient(30 * time.Second)
	}
	evmSigner, err := signclient.NewRemoteEVMSigner(digestClient)
	if err != nil {
		slog.Warn("external EVM signer is invalid, falling back to GenericHttpClient", "err", err)
		return NewGenericHttpClient(30 * time.Second)
	}
	paymentClient := x402.Newx402Client().Register("eip155:*", exactevm.NewExactEvmScheme(evmSigner, nil))
	headerEncoder := x402http.Newx402HTTPClient(paymentClient)
	client, err := NewX402PaymentHttpClient(X402Config{
		HTTPTimeout:  timeout,
		PayerAddress: payCfg.PublicAddress,
	}, eng, paymentClient, headerEncoder, nil)
	if err != nil {
		slog.Warn("x402 HTTP client is invalid, falling back to GenericHttpClient", "err", err)
		return NewGenericHttpClient(30 * time.Second)
	}

	slog.Info("X402PaymentHttpClient created", "timeout", timeout, "wallet_transport", payCfg.Transport)

	return client
}

func resolveSignTimeout(payCfg *config.WalletConfig) time.Duration {
	if payCfg.SignTimeout != "" {
		if duration, err := time.ParseDuration(payCfg.SignTimeout); err == nil && duration > 0 {
			return duration
		}
	}
	return 30 * time.Second
}

func resolveTimeout(payCfg *config.WalletConfig) time.Duration {
	if payCfg.X402.Timeout != "" {
		if d, err := time.ParseDuration(payCfg.X402.Timeout); err == nil {
			return d
		}
	}
	return 30 * time.Second
}
