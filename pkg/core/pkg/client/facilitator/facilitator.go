//go:build x402

// Package facilitator implements the x402 facilitator HTTP client.
package facilitator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	x402 "github.com/x402-foundation/x402/go"
	"github.com/x402-foundation/x402/go/types"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// Client is the x402 facilitator HTTP client.
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

// Health checks the facilitator's health endpoint.
func (c *Client) Health(ctx context.Context) error {
	if c.endpoint == "" {
		return &model.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("facilitator health request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("facilitator health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &model.PaymentError{
			Code: "FACILITATOR_UNHEALTHY", Message: fmt.Sprintf("health returned status %d", resp.StatusCode),
		}
	}
	return nil
}

// Supported returns the facilitator's supported schemes and networks.
func (c *Client) Supported(ctx context.Context) (*x402.SupportedResponse, error) {
	if c.endpoint == "" {
		return nil, &model.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/supported", nil)
	if err != nil {
		return nil, fmt.Errorf("facilitator supported request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("facilitator supported: %w", err)
	}
	defer resp.Body.Close()

	var result x402.SupportedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode supported response: %w", err)
	}
	return &result, nil
}

// Verify sends a payment verification request to the facilitator.
func (c *Client) Verify(ctx context.Context, req *types.PaymentRequirements) (*x402.VerifyResponse, error) {
	if c.endpoint == "" {
		return nil, &model.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal verify request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/verify", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create verify request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	var vr x402.VerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decode verify response: %w", err)
	}
	return &vr, nil
}

// Authorize sends a signed PaymentPayload to the facilitator's /settle endpoint.
func (c *Client) Authorize(ctx context.Context, payload *types.PaymentPayload) (*model.PaymentReceipt, error) {
	if c.endpoint == "" {
		return nil, &model.PaymentError{
			Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured",
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal settle payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/settle", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create settle request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("settle request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &model.PaymentError{
			Code: "FACILITATOR_ERROR", Message: fmt.Sprintf("settlement returned status %d", resp.StatusCode),
		}
	}

	var sr x402.SettleResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode settle response: %w", err)
	}

	return &model.PaymentReceipt{
		ID:            payload.Accepted.PayTo + "-" + sr.Transaction,
		IntentID:      payload.Accepted.PayTo + "-" + sr.Transaction,
		TxHash:        sr.Transaction,
		Authorization: sr.Transaction,
		PaidAt:        time.Now(),
	}, nil
}
