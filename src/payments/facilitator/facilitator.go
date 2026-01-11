// Package facilitator implements the x402 facilitator HTTP client.
// Flowgent sends verify and settle requests to the facilitator,
// which handles onchain settlement.
//
// Protocol endpoints:
//   - GET  /health     — health check
//   - GET  /supported  — list supported payment schemes
//   - POST /verify     — verify a proposed x402 payment
//   - POST /settle     — settle a verified payment on-chain
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

// ─── Health ──────────────────────────────────────────────────

// Health checks if the facilitator is reachable.
func (c *Client) Health(ctx context.Context) error {
	if c.endpoint == "" {
		return &payments.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
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
		return &payments.PaymentError{
			Code: "FACILITATOR_UNHEALTHY", Message: fmt.Sprintf("health returned status %d", resp.StatusCode),
		}
	}
	return nil
}

// ─── Supported ───────────────────────────────────────────────

// SupportedNetworks returns the list of supported payment networks from the facilitator.
func (c *Client) SupportedNetworks(ctx context.Context) (map[string]any, error) {
	if c.endpoint == "" {
		return nil, &payments.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
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

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode supported response: %w", err)
	}
	return result, nil
}

// ─── Verify ────────────────────────────────────────────────

// VerifyResponse is the response from POST /verify.
type VerifyResponse struct {
	IsValid   bool   `json:"isValid"`
	Reason    string `json:"invalidReason,omitempty"`
	Message   string `json:"invalidMessage,omitempty"`
}

// VerifyRequest is the body sent to POST /verify.
type VerifyRequest struct {
	Scheme    string `json:"scheme"`
	Network   string `json:"network"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Asset     string `json:"asset"`
}

// Verify sends a payment verification request to the facilitator.
func (c *Client) Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error) {
	if c.endpoint == "" {
		return nil, &payments.PaymentError{Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured"}
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

	var vr VerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decode verify response: %w", err)
	}
	return &vr, nil
}

// ─── Settle (Authorize) ─────────────────────────────────────

// SettleRequest is the body sent to POST /settle.
type SettleRequest struct {
	Network   string `json:"network"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Signature string `json:"signature"`
}

// SettleResponse is the response from POST /settle.
type SettleResponse struct {
	TxHash string `json:"txHash,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Authorize is the legacy API — it POSTs signed PaymentAuthorization to the facilitator.
// For real facilitators, it sends a SettleRequest to POST /settle.
func (c *Client) Authorize(ctx context.Context, auth *payments.PaymentAuthorization) (*payments.PaymentReceipt, error) {
	if c.endpoint == "" {
		return nil, &payments.PaymentError{
			Code: "FACILITATOR_NOT_CONFIGURED", Message: "no facilitator endpoint configured",
		}
	}

	settleReq := &SettleRequest{
		Network:   "",  // derived from payment request
		Recipient: "",  // derived from payment request
		Amount:    "",  // derived from payment request
		Signature: auth.Signature,
	}

	body, err := json.Marshal(settleReq)
	if err != nil {
		return nil, fmt.Errorf("marshal settle request: %w", err)
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
		return nil, &payments.PaymentError{
			Code: "FACILITATOR_ERROR", Message: fmt.Sprintf("settlement returned status %d", resp.StatusCode),
		}
	}

	var sr SettleResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode settle response: %w", err)
	}

	return &payments.PaymentReceipt{
		ID:            auth.IntentID,
		IntentID:      auth.IntentID,
		TxHash:        sr.TxHash,
		Authorization: auth.Signature,
		PaidAt:        time.Now(),
	}, nil
}
