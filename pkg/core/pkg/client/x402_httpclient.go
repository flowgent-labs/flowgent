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

	"github.com/flowgent-labs/flowgent/core/pkg/client/policy"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// ApprovalHandler gates payment signing when policy requires human approval.
// The returned receipt records the approval decision.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, intent *model.PaymentIntent) (*model.PaymentReceipt, error)
}

// PaymentClient selects a supported requirement and creates its signed x402
// payload. Only its final EIP-712 digest is delegated to the Wallet service.
type PaymentClient interface {
	SelectPaymentRequirements([]types.PaymentRequirements) (types.PaymentRequirements, error)
	CreatePaymentPayload(context.Context, types.PaymentRequirements, *types.ResourceInfo, map[string]interface{}) (types.PaymentPayload, error)
}

// PaymentHeaderEncoder converts the signed payload into protocol-version-aware
// HTTP headers. The official x402 HTTP adapter implements this interface.
type PaymentHeaderEncoder interface {
	EncodePaymentSignatureHeader([]byte) (map[string]string, error)
}

// X402Config configures one x402-aware HTTP client.
type X402Config struct {
	HTTPTimeout  time.Duration
	PayerAddress string
}

// X402PaymentHttpClient applies payment policy, requests a signature from the
// external Wallet, and retries a resource once with the standard x402 payment
// header. The resource server, not Flowgent, owns facilitator settlement.
type X402PaymentHttpClient struct {
	httpClient    *http.Client
	policyEngine  *policy.Engine
	paymentClient PaymentClient
	headerEncoder PaymentHeaderEncoder
	approver      ApprovalHandler
	payerAddress  string
}

// NewX402PaymentHttpClient creates an x402-aware HTTP client.
func NewX402PaymentHttpClient(
	cfg X402Config,
	policyEngine *policy.Engine,
	paymentClient PaymentClient,
	headerEncoder PaymentHeaderEncoder,
	approver ApprovalHandler,
) (*X402PaymentHttpClient, error) {
	if policyEngine == nil {
		return nil, fmt.Errorf("x402: policy engine is required")
	}
	if paymentClient == nil {
		return nil, fmt.Errorf("x402: payment client is required")
	}
	if headerEncoder == nil {
		return nil, fmt.Errorf("x402: payment header encoder is required")
	}
	if cfg.PayerAddress == "" {
		return nil, fmt.Errorf("x402: payer address is required")
	}
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
		policyEngine:  policyEngine,
		paymentClient: paymentClient,
		headerEncoder: headerEncoder,
		approver:      approver,
		payerAddress:  cfg.PayerAddress,
	}, nil
}

// Do executes an HTTP request with one bounded x402 payment retry.
func (c *X402PaymentHttpClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x402: request failed: %w", err)
	}
	if !IsX402Response(resp) {
		return resp, nil
	}

	paymentRequired, err := Parse(resp)
	closeResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("x402: parse 402 response: %w", err)
	}

	selected, err := c.paymentClient.SelectPaymentRequirements(paymentRequired.Accepts)
	if err != nil {
		return nil, fmt.Errorf("x402: select payment requirement: %w", err)
	}
	amount, err := ParsePaymentAmountUSD(selected)
	if err != nil {
		return nil, fmt.Errorf("x402: validate payment requirement: %w", err)
	}

	retryReq, err := cloneForPaymentRetry(req)
	if err != nil {
		return nil, err
	}
	intent := &model.PaymentIntent{
		ID:        uuid.NewString(),
		URL:       req.URL.String(),
		Payer:     c.payerAddress,
		Asset:     selected.Asset,
		Amount:    amount,
		Chain:     selected.Network,
		Recipient: selected.PayTo,
		Status:    model.IntentPending,
		CreatedAt: time.Now(),
	}

	if err := c.policyEngine.Allow(req.Context(), intent); err != nil {
		intent.Status = model.IntentDenied
		return nil, err
	}
	if c.policyEngine.RequiresHumanApproval(intent) {
		if c.approver == nil {
			return nil, model.ErrPaymentRequiresApproval
		}
		if _, err := c.approver.RequestApproval(req.Context(), intent); err != nil {
			intent.Status = model.IntentDenied
			return nil, fmt.Errorf("x402: approval denied: %w", err)
		}
	}
	intent.Status = model.IntentApproved

	payload, err := c.paymentClient.CreatePaymentPayload(
		req.Context(),
		selected,
		paymentRequired.Resource,
		paymentRequired.Extensions,
	)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: create signed payment payload: %w", err)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: encode signed payment payload: %w", err)
	}
	paymentHeaders, err := c.headerEncoder.EncodePaymentSignatureHeader(payloadBytes)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: encode payment header: %w", err)
	}
	for name, value := range paymentHeaders {
		retryReq.Header.Set(name, value)
	}

	// Reserve the authorization amount before dispatch. This intentionally
	// counts an ambiguous network outcome against the local safety budget.
	if err := c.policyEngine.ReserveSpend(req.Context(), c.payerAddress, intent.Amount); err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: reserve payment budget: %w", err)
	}

	retryResp, err := c.httpClient.Do(retryReq)
	if err != nil {
		intent.Status = model.IntentFailed
		return nil, fmt.Errorf("x402: payment retry failed: %w", err)
	}
	if IsX402Response(retryResp) {
		intent.Status = model.IntentFailed
	} else {
		intent.Status = model.IntentPaid
	}
	return retryResp, nil
}

func cloneForPaymentRetry(req *http.Request) (*http.Request, error) {
	retryReq := req.Clone(req.Context())
	switch {
	case req.Body == nil:
		retryReq.Body = nil
	case req.Body == http.NoBody:
		retryReq.Body = http.NoBody
	case req.GetBody != nil:
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("x402: recreate request body: %w", err)
		}
		retryReq.Body = body
	default:
		return nil, fmt.Errorf("x402: request body is not replayable")
	}
	return retryReq, nil
}

func closeResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// Get performs a GET request with x402 payment awareness.
func (c *X402PaymentHttpClient) Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
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
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return c.Do(req)
}

var _ model.IFlowgentAPIClient = (*X402PaymentHttpClient)(nil)
