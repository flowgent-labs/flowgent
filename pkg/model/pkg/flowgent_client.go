// Package model provides the IFlowgentAPIClient abstraction for all outbound
// HTTP calls. Implementations include a standard HTTP client and an x402
// payment-aware client (behind the x402 build tag).
package model

import (
	"context"
	"net/http"
)

// IFlowgentAPIClient is the unified HTTP client interface for all Flowgent
// components. All outbound HTTP calls MUST use this interface rather than
// raw *http.Client or http.DefaultClient.
//
// At startup, one of two implementations is injected based on wallet
// config (wallet.enabled):
//   - GenericHttpClient — standard HTTP with timeouts and tracing
//   - X402PaymentHttpClient — wraps GenericHttpClient with x402 payment handling
type IFlowgentAPIClient interface {
	// Do executes an HTTP request. If the implementation is x402-aware and
	// the server returns 402 Payment Required, the payment flow is handled
	// transparently and the final response (after payment) is returned.
	Do(req *http.Request) (*http.Response, error)

	// Get is a convenience wrapper for GET requests with custom headers.
	Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error)

	// Post is a convenience wrapper for POST requests with JSON body and custom headers.
	Post(ctx context.Context, url string, body []byte, headers map[string]string) (*http.Response, error)
}
