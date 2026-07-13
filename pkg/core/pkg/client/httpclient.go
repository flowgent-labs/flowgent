package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// GenericHttpClient is the standard HTTP client implementation. It wraps
// *http.Client with reasonable defaults (timeouts, connection pooling).
// This is the default IFlowgentAPIClient injected when payments are disabled.
type GenericHttpClient struct {
	client *http.Client
}

// NewGenericHttpClient creates a GenericHttpClient with the given timeout.
func NewGenericHttpClient(timeout time.Duration) *GenericHttpClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &GenericHttpClient{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false,
			},
		},
	}
}

// Do executes the request via the wrapped http.Client.
func (c *GenericHttpClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

// Get performs a GET request with custom headers.
func (c *GenericHttpClient) Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client.Do(req)
}

// Post performs a POST request with a JSON body and custom headers.
func (c *GenericHttpClient) Post(ctx context.Context, url string, body []byte, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client.Do(req)
}

// readBody reads and closes the response body, returning the bytes.
func readBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

var _ model.IFlowgentAPIClient = (*GenericHttpClient)(nil)
