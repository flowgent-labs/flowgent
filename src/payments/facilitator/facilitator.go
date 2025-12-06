// Package facilitator implements the Coinbase x402 facilitator client.
// Flowgent sends signed payment authorizations to the facilitator,
// which handles onchain settlement.
package facilitator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/flowgent-labs/flowgent/src/payments"
)

// Client is the Coinbase x402 facilitator HTTP client.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// New creates a new facilitator client.
func New(endpoint string, timeout time.Duration) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Authorize sends a signed payment authorization to the facilitator
// and returns a payment receipt if settlement succeeds.
func (c *Client) Authorize(ctx context.Context, auth *payments.PaymentAuthorization) (*payments.PaymentReceipt, error) {
	if c.endpoint == "" {
		return nil, &payments.PaymentError{
			Code:    "FACILITATOR_NOT_CONFIGURED",
			Message: "no facilitator endpoint configured",
		}
	}

	body, err := json.Marshal(auth)
	if err != nil {
		return nil, fmt.Errorf("marshal authorization: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/authorize", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create facilitator request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facilitator request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &payments.PaymentError{
			Code:    "FACILITATOR_ERROR",
			Message: fmt.Sprintf("facilitator returned status %d", resp.StatusCode),
		}
	}

	var receipt payments.PaymentReceipt
	if err := json.NewDecoder(resp.Body).Decode(&receipt); err != nil {
		return nil, fmt.Errorf("decode facilitator response: %w", err)
	}

	return &receipt, nil
}

// Health checks if the facilitator is reachable.
func (c *Client) Health(ctx context.Context) error {
	if c.endpoint == "" {
		return fmt.Errorf("facilitator endpoint not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("facilitator health check returned %d", resp.StatusCode)
	}
	return nil
}
