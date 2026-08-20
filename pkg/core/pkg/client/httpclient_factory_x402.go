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
// When payments are disabled, it returns the standard HTTP client.
func NewHttpClient(cfg *config.FlowgentConfig, _ messager.IMessager) (model.IFlowgentAPIClient, error) {
	if cfg == nil || cfg.Wallet == nil || !cfg.Wallet.Enabled {
		return NewGenericHttpClient(30 * time.Second), nil
	}

	payCfg := cfg.Wallet

	timeout := resolveTimeout(payCfg)

	// Policy engine
	eng := policy.NewEngine(&payCfg.Policies, nil)

	if payCfg.KeyID == "" || payCfg.PublicAddress == "" {
		return nil, fmt.Errorf("wallet client identity requires key_id and public_address")
	}

	signTimeout := resolveSignTimeout(payCfg)
	clientIDPrefix := payCfg.ClientIDPrefix
	if clientIDPrefix == "" {
		clientIDPrefix = "flowgent-"
	}
	clientID := clientIDPrefix + uuid.NewString()
	if err := signclient.ValidateIdentity(clientID, payCfg.KeyID, payCfg.PublicAddress); err != nil {
		return nil, fmt.Errorf("validate external wallet identity: %w", err)
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
		return nil, fmt.Errorf("create external wallet transport: %w", transportErr)
	}
	digestClient, err := signclient.NewDigestClient(transport, clientID, payCfg.KeyID, payCfg.PublicAddress, signTimeout)
	if err != nil {
		return nil, fmt.Errorf("create external wallet client: %w", err)
	}
	evmSigner, err := signclient.NewRemoteEVMSigner(digestClient)
	if err != nil {
		return nil, fmt.Errorf("create external EVM signer: %w", err)
	}
	paymentClient := x402.Newx402Client().Register("eip155:*", exactevm.NewExactEvmScheme(evmSigner, nil))
	headerEncoder := x402http.Newx402HTTPClient(paymentClient)
	client, err := NewX402PaymentHttpClient(X402Config{
		HTTPTimeout:  timeout,
		PayerAddress: payCfg.PublicAddress,
	}, eng, paymentClient, headerEncoder, nil)
	if err != nil {
		return nil, fmt.Errorf("create x402 HTTP client: %w", err)
	}

	slog.Info("X402PaymentHttpClient created", "timeout", timeout, "wallet_transport", payCfg.Transport)

	return client, nil
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
