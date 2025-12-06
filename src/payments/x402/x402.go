// Package x402 provides parsing and validation for x402 payment protocol responses.
package x402

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/src/payments"
)

const (
	HeaderX402Payment = "X402-Payment"
	HeaderX402Auth    = "X402-Authorization"
)

// Parse extracts an X402PaymentRequest from an HTTP 402 response.
// It reads the X402-Payment header and unmarshals the JSON body.
func Parse(resp *http.Response) (*payments.X402PaymentRequest, error) {
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, fmt.Errorf("x402: expected 402 status, got %d", resp.StatusCode)
	}

	headerVal := resp.Header.Get(HeaderX402Payment)
	if headerVal == "" {
		return nil, fmt.Errorf("x402: missing %s header", HeaderX402Payment)
	}

	var pr payments.X402PaymentRequest
	if err := json.Unmarshal([]byte(headerVal), &pr); err != nil {
		return nil, fmt.Errorf("x402: invalid %s header: %w", HeaderX402Payment, err)
	}

	if err := pr.Validate(); err != nil {
		return nil, fmt.Errorf("x402: validation failed: %w", err)
	}

	return &pr, nil
}

// ParseFromResponse is a convenience function that checks for a 402 status
// and parses the x402 payment request if present. Returns nil if not a payment request.
func ParseFromResponse(resp *http.Response) (*payments.X402PaymentRequest, error) {
	if resp.StatusCode != http.StatusPaymentRequired {
		return nil, nil
	}
	return Parse(resp)
}

// SetAuthorizationHeader adds the x402 authorization token to an HTTP request.
func SetAuthorizationHeader(req *http.Request, token string) {
	req.Header.Set(HeaderX402Auth, token)
}

// IsX402Response checks if an HTTP response is an x402 payment request.
func IsX402Response(resp *http.Response) bool {
	return resp.StatusCode == http.StatusPaymentRequired &&
		resp.Header.Get(HeaderX402Payment) != ""
}

// FormatPaymentHeader serializes the payment request to the X402-Payment header format.
func FormatPaymentHeader(pr *payments.X402PaymentRequest) (string, error) {
	data, err := json.Marshal(pr)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseAssetAmount parses a decimal amount from a string.
func ParseAssetAmount(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(strings.TrimSpace(s))
}
