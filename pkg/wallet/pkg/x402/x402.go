// Package x402 provides parsing and validation for x402 payment protocol responses.
// Uses the official x402 SDK types (github.com/x402-foundation/x402/go/types) for
// protocol-level structures (PaymentRequired, PaymentRequirements, PaymentPayload).
package x402

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/types"
)

const (
	HeaderX402Auth = "X402-Authorization"
)

// Parse extracts a V2 PaymentRequired from an HTTP 402 response body.
// Falls back to V1 header parsing if the body isn't valid V2 JSON.
func Parse(resp *http.Response) (*types.PaymentRequired, error) {
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, fmt.Errorf("x402: expected 402 status, got %d", resp.StatusCode)
	}

	// Try V2 body parsing first
	if resp.Body != nil {
		var pr types.PaymentRequired
		if err := json.NewDecoder(resp.Body).Decode(&pr); err == nil && pr.X402Version >= 2 {
			if len(pr.Accepts) == 0 {
				return nil, fmt.Errorf("x402: PaymentRequired has empty accepts array")
			}
			return &pr, nil
		}
		// If V2 parse fails, try V1 header fallback below
	}

	// V1 fallback: parse X402-Payment header
	return parseV1Header(resp)
}

// parseV1Header parses a V1-style X402-Payment header into a PaymentRequired.
func parseV1Header(resp *http.Response) (*types.PaymentRequired, error) {
	const headerX402Payment = "X402-Payment"
	headerVal := resp.Header.Get(headerX402Payment)
	if headerVal == "" {
		return nil, fmt.Errorf("x402: missing %s header and body is not valid V2", headerX402Payment)
	}

	var v1 struct {
		Asset       string `json:"asset"`
		Amount      string `json:"amount"`
		Chain       string `json:"chain"`
		Recipient   string `json:"recipient"`
		Settlement  string `json:"settlement"`
		Facilitator string `json:"facilitator"`
	}
	if err := json.Unmarshal([]byte(headerVal), &v1); err != nil {
		return nil, fmt.Errorf("x402: invalid %s header: %w", headerX402Payment, err)
	}

	if v1.Asset == "" || v1.Amount == "" || v1.Recipient == "" {
		return nil, fmt.Errorf("x402: V1 header missing required fields")
	}

	return &types.PaymentRequired{
		X402Version: 1,
		Accepts: []types.PaymentRequirements{{
			Scheme:  v1.Settlement,
			Network: v1.Chain,
			Asset:   v1.Asset,
			Amount:  v1.Amount,
			PayTo:   v1.Recipient,
		}},
	}, nil
}

// SetAuthorizationHeader adds the x402 authorization token to an HTTP request.
func SetAuthorizationHeader(req *http.Request, token string) {
	req.Header.Set(HeaderX402Auth, token)
}

// IsX402Response checks if an HTTP response is an x402 payment request (402 status).
func IsX402Response(resp *http.Response) bool {
	return resp.StatusCode == http.StatusPaymentRequired
}

// FirstAccept returns the first accepted payment requirement, or nil if empty.
func FirstAccept(pr *types.PaymentRequired) *types.PaymentRequirements {
	if pr == nil || len(pr.Accepts) == 0 {
		return nil
	}
	return &pr.Accepts[0]
}

// ParseAssetAmount parses a decimal amount from a string.
func ParseAssetAmount(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(strings.TrimSpace(s))
}
