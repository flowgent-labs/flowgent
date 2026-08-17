//go:build x402

package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/mechanisms/evm"
	"github.com/x402-foundation/x402/go/types"
)

const (
	HeaderPaymentRequired  = "PAYMENT-REQUIRED"
	maxPaymentRequiredSize = 64 << 10
)

// Parse extracts and validates a V2 PaymentRequired response. The canonical V2
// representation is the base64-encoded PAYMENT-REQUIRED header; a JSON body is
// accepted for compatibility with resource servers that have not migrated yet.
func Parse(resp *http.Response) (*types.PaymentRequired, error) {
	if resp == nil {
		return nil, fmt.Errorf("x402: response is required")
	}
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, fmt.Errorf("x402: expected 402 status, got %d", resp.StatusCode)
	}

	raw, err := paymentRequiredBytes(resp)
	if err != nil {
		return nil, err
	}

	var required types.PaymentRequired
	if err := json.Unmarshal(raw, &required); err != nil {
		return nil, fmt.Errorf("x402: decode payment requirements: %w", err)
	}
	if required.X402Version != 2 {
		return nil, fmt.Errorf("x402: unsupported protocol version %d", required.X402Version)
	}
	if len(required.Accepts) == 0 {
		return nil, fmt.Errorf("x402: PaymentRequired has empty accepts array")
	}
	return &required, nil
}

func paymentRequiredBytes(resp *http.Response) ([]byte, error) {
	if encoded := strings.TrimSpace(resp.Header.Get(HeaderPaymentRequired)); encoded != "" {
		if base64.StdEncoding.DecodedLen(len(encoded)) > maxPaymentRequiredSize {
			return nil, fmt.Errorf("x402: %s header exceeds %d bytes", HeaderPaymentRequired, maxPaymentRequiredSize)
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("x402: decode %s header: %w", HeaderPaymentRequired, err)
		}
		return raw, nil
	}

	if resp.Body == nil {
		return nil, fmt.Errorf("x402: missing %s header and response body", HeaderPaymentRequired)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPaymentRequiredSize+1))
	if err != nil {
		return nil, fmt.Errorf("x402: read payment requirements: %w", err)
	}
	if len(raw) > maxPaymentRequiredSize {
		return nil, fmt.Errorf("x402: payment requirements body exceeds %d bytes", maxPaymentRequiredSize)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("x402: missing %s header and response body", HeaderPaymentRequired)
	}
	return raw, nil
}

// IsX402Response reports whether a response requests x402 payment.
func IsX402Response(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusPaymentRequired
}

// ParsePaymentAmountUSD converts an EVM requirement's atomic-unit amount to a
// decimal USD value. Flowgent intentionally supports only the network's known
// default stablecoin so an unknown token cannot bypass USD-denominated policy.
func ParsePaymentAmountUSD(requirement types.PaymentRequirements) (decimal.Decimal, error) {
	network, err := evm.GetNetworkConfig(requirement.Network)
	if err != nil || network.DefaultAsset.Address == "" {
		return decimal.Zero, fmt.Errorf("network %q has no known default stablecoin", requirement.Network)
	}
	if !strings.EqualFold(requirement.Asset, network.DefaultAsset.Address) {
		return decimal.Zero, fmt.Errorf(
			"asset %q is not the known default stablecoin for %s",
			requirement.Asset,
			requirement.Network,
		)
	}
	if network.DefaultAsset.Decimals < 0 || network.DefaultAsset.Decimals > 36 {
		return decimal.Zero, fmt.Errorf("asset decimal precision is unsupported")
	}

	amount, err := decimal.NewFromString(strings.TrimSpace(requirement.Amount))
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid payment amount %q: %w", requirement.Amount, err)
	}
	if !amount.IsPositive() || amount.Exponent() < 0 {
		return decimal.Zero, fmt.Errorf("payment amount must be a positive atomic-unit integer")
	}
	return amount.Shift(-int32(network.DefaultAsset.Decimals)), nil
}
