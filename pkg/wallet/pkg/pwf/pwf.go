// Package pwf implements the payable web fetch runtime — a core economic-aware
// HTTP fetch primitive that handles x402 payment responses transparently.
//
// PWF is NOT just a tool wrapper. It is a core economic-aware fetch runtime
// that sits alongside the orchestration engine.
package pwf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/x402-foundation/x402/go/types"

	"github.com/flowgent-labs/flowgent/wallet/src"
	"github.com/flowgent-labs/flowgent/wallet/src/facilitator"
	"github.com/flowgent-labs/flowgent/wallet/src/policy"
	"github.com/flowgent-labs/flowgent/wallet/src/wallet"
	"github.com/flowgent-labs/flowgent/wallet/src/x402"
)

// Runtime is the payable web fetch runtime. It wraps an HTTP client with
// x402 payment awareness, policy evaluation, and facilitator integration.
type Runtime struct {
	httpClient   *http.Client
	policyEngine *policy.Engine
	walletMgr    *wallet.Manager
	facilitator  *facilitator.Client
	approver     ApprovalHandler
	defaultAddr  string
}

// ApprovalHandler is called when a payment requires human approval.
// It returns nil if approved, or an error if rejected/expired.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, intent *payments.PaymentIntent) (*payments.PaymentReceipt, error)
}

// Config configures the PWF runtime.
type Config struct {
	HTTPTimeout        time.Duration
	MaxRetries         int
	DefaultFacilitator string
}

// New creates a new PWF runtime.
func New(cfg Config, policyEngine *policy.Engine, walletMgr *wallet.Manager, fClient *facilitator.Client, approver ApprovalHandler) *Runtime {
	return &Runtime{
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
		policyEngine: policyEngine,
		walletMgr:    walletMgr,
		facilitator:  fClient,
		approver:     approver,
		defaultAddr:  "",
	}
}

// Fetch performs an HTTP request with x402 payment awareness.
// If the server responds with 402 Payment Required, the runtime:
//  1. Parses the x402 payment request
//  2. Creates a PaymentIntent
//  3. Evaluates spending policies
//  4. Requests human approval if above threshold
//  5. Signs payment authorization via wallet
//  6. Sends authorization to facilitator
//  7. Retries the original request with the payment token
func (r *Runtime) Fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	return r.fetchWithRetry(ctx, req, 0)
}

func (r *Runtime) fetchWithRetry(ctx context.Context, req *http.Request, attempt int) (*http.Response, error) {
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pwf: request failed: %w", err)
	}

	// Not a payment request — return response as-is
	if !x402.IsX402Response(resp) {
		return resp, nil
	}

	// Parse x402 payment request (uses official SDK types.PaymentRequired)
	paymentReq, err := x402.Parse(resp)
	if err != nil {
		return nil, fmt.Errorf("pwf: parse x402 response: %w", err)
	}
	accept := x402.FirstAccept(paymentReq)
	if accept == nil {
		return nil, fmt.Errorf("pwf: no payment option in 402 response")
	}

	// Create payment intent (flowgent-internal tracking)
	amt, _ := x402.ParseAssetAmount(accept.Amount)
	intent := &payments.PaymentIntent{
		ID:          uuid.NewString(),
		URL:         req.URL.String(),
		Asset:       accept.Asset,
		Amount:      amt,
		Chain:       accept.Network,
		Recipient:   accept.PayTo,
		Facilitator: req.URL.Host, // V2: facilitator is the server that returned 402
		Status:      payments.IntentPending,
		CreatedAt:   time.Now(),
	}

	// Evaluate policy
	if err := r.policyEngine.Allow(ctx, intent); err != nil {
		intent.Status = payments.IntentDenied
		return nil, err
	}

	// Human approval if required
	if r.policyEngine.RequiresHumanApproval(intent) {
		if r.approver == nil {
			return nil, payments.ErrPaymentRequiresApproval
		}
		receipt, err := r.approver.RequestApproval(ctx, intent)
		if err != nil {
			intent.Status = payments.IntentDenied
			return nil, fmt.Errorf("pwf: approval denied: %w", err)
		}
		if receipt != nil {
			// Approval returned a receipt directly (e.g., pre-authorized)
			return r.retryWithToken(ctx, req, receipt.Authorization)
		}
	}

	// Build V2 PaymentPayload for the facilitator using the SDK types from 402 response
	payload := &types.PaymentPayload{
		X402Version: 2,
		Payload: map[string]interface{}{
			"intent_id": intent.ID,
			"url":       intent.URL,
		},
		Accepted: *accept, // reuse the PaymentRequirements from the 402 response
	}

	// Sign the payload
	payloadBytes, _ := json.Marshal(payload)
	wallet, err := r.walletMgr.Get(r.defaultAddr)
	if err != nil {
		intent.Status = payments.IntentFailed
		return nil, fmt.Errorf("pwf: get wallet: %w", err)
	}
	sig, err := wallet.SignAuthorization(ctx, payloadBytes)
	if err != nil {
		intent.Status = payments.IntentFailed
		return nil, fmt.Errorf("pwf: sign authorization: %w", err)
	}
	payload.Payload["signature"] = string(sig)

	// Send to facilitator
	receipt, err := r.facilitator.Authorize(ctx, payload)
	if err != nil {
		intent.Status = payments.IntentFailed
		return nil, fmt.Errorf("pwf: facilitator authorize: %w", err)
	}

	intent.Status = payments.IntentPaid

	// Record spend for budget tracking
	_ = r.policyEngine.RecordSpend(ctx, r.defaultAddr, intent.Amount)

	// Retry original request with payment token
	return r.retryWithToken(ctx, req, receipt.Authorization)
}

func (r *Runtime) retryWithToken(ctx context.Context, req *http.Request, token string) (*http.Response, error) {
	// Clone the request for retry
	retryReq := req.Clone(ctx)
	x402.SetAuthorizationHeader(retryReq, token)
	return r.httpClient.Do(retryReq)
}

// SetDefaultWallet sets the default wallet address used for payments.
func (r *Runtime) SetDefaultWallet(addr string) {
	r.defaultAddr = addr
}
