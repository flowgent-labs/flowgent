//go:build x402

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/x402-foundation/x402/go/types"

	model "github.com/flowgent-labs/flowgent/model/pkg"
	"github.com/flowgent-labs/flowgent/core/pkg/client/facilitator"
	"github.com/flowgent-labs/flowgent/core/pkg/client/policy"
)

// ApprovalHandler is called when a payment requires human approval.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, intent *model.PaymentIntent) (*model.PaymentReceipt, error)
}

// X402Config configures the x402 payment HTTP client.
type X402Config struct {
	HTTPTimeout time.Duration
	MaxRetries  int
}

// X402PaymentHttpClient is a self-contained, pluggable implementation of
// IFlowgentHttpClient with transparent x402 payment handling. When a server
// returns 402 Payment Required, the full payment flow (policy check, signing,
// facilitator settlement) is handled transparently and the request is retried
// with the payment token.
type X402PaymentHttpClient struct {
	httpClient   *http.Client
	policyEngine *policy.Engine
	facilitator  *facilitator.Client
	signClient   model.SignClient
	approver     ApprovalHandler
	defaultAddr  string
}

// NewX402PaymentHttpClient creates an x402-aware HTTP client.
func NewX402PaymentHttpClient(cfg X402Config, policyEngine *policy.Engine, fClient *facilitator.Client, signClient model.SignClient, approver ApprovalHandler) *X402PaymentHttpClient {
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	return &X402PaymentHttpClient{
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:       100,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: false,
			},
		},
		policyEngine: policyEngine,
		facilitator:  fClient,
		signClient:   signClient,
		approver:     approver,
		defaultAddr:  "",
	}
}

// SetDefaultWallet sets the default wallet address used for payments.
func (c *X402PaymentHttpClient) SetDefaultWallet(addr string) {
	c.defaultAddr = addr
}

// Do executes an HTTP request with x402 payment awareness.
// If the server returns 402 Payment Required, the full payment flow is handled
// transparently: parse 402, evaluate policy, get approval, sign, settle, retry.
func (c *X402PaymentHttpClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x402: request failed: %w", err)
	}

	if !IsX402Response(resp) {
		return resp, nil
	}

	// Parse x402 payment request
	paymentReq, err := Parse(resp)
	if err != nil {
		return nil, fmt.Errorf("x402: parse 402 response: %w", err)
	}
	accept := FirstAccept(paymentReq)
	if accept == nil {
		return nil, fmt.Errorf("x402: no payment option in 402 response")
	}

	// Create payment intent
	amt, _ := ParseAssetAmount(accept.Amount)
	intent := &model.PaymentIntent{
		ID:          uuid.NewString(),
		URL:         req.URL.String(),
		Asset:       accept.Asset,
		Amount:      amt,
		Chain:       accept.Network,
		Recipient:   accept.PayTo,
		Facilitator: req.URL.Host,
		Status:      model.IntentPending,
		CreatedAt:   time.Now(),
	}

	// Evaluate policy
	if err := c.policyEngine.Allow(req.Context(), intent); err != nil {
		intent.Status = model.IntentDenied
		return nil, err
	}

	// Human approval if required
	if c.policyEngine.RequiresHumanApproval(intent) {
		if c.approver == nil {
			return nil, model.ErrPaymentRequiresApproval
		}
		receipt, err := c.approver.RequestApproval(req.Context(), intent)
		if err != nil {
			intent.Status = model.IntentDenied
			return nil, fmt.Errorf("x402: approval denied: %w", err)
		}
		if receipt != nil {
			return c.retryWithToken(req, receipt.Authorization)
		}
	}

	// Build PaymentPayload
	payload := &types.PaymentPayload{
		X402Version: 2,
		Payload: map[string]interface{}{
			"intent_id": intent.ID,
			"url":       intent.URL,
		},
		Accepted: *accept,
	}

	// Sign the payload via the configured SignClient
	payloadBytes, _ := json.Marshal(payload)
	sig, err := c.signClient.Sign(req.Context(), c.defaultAddr, payloadBytes)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: sign authorization: %w", err)
	}
	payload.Payload["signature"] = string(sig)

	// Send to facilitator
	receipt, err := c.facilitator.Authorize(req.Context(), payload)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: facilitator authorize: %w", err)
	}

	intent.Status = model.IntentPaid
	_ = c.policyEngine.RecordSpend(req.Context(), c.defaultAddr, intent.Amount)

	// Retry original request with payment token
	return c.retryWithToken(req, receipt.Authorization)
}

func (c *X402PaymentHttpClient) retryWithToken(req *http.Request, token string) (*http.Response, error) {
	retryReq := req.Clone(req.Context())
	SetAuthorizationHeader(retryReq, token)
	return c.httpClient.Do(retryReq)
}

// Get performs a GET request with x402 payment awareness.
func (c *X402PaymentHttpClient) Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.Do(req)
}

// Post performs a POST request with x402 payment awareness.
func (c *X402PaymentHttpClient) Post(ctx context.Context, url string, body []byte, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.Do(req)
}

var _ model.IFlowgentHttpClient = (*X402PaymentHttpClient)(nil)
