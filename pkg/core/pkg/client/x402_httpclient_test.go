//go:build x402

package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"
	x402 "github.com/x402-foundation/x402/go"
	x402http "github.com/x402-foundation/x402/go/http"
	"github.com/x402-foundation/x402/go/types"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/core/pkg/client/policy"
)

type stubPaymentClient struct {
	createCalls atomic.Int32
}

func (c *stubPaymentClient) SelectPaymentRequirements(requirements []types.PaymentRequirements) (types.PaymentRequirements, error) {
	return requirements[0], nil
}

func (c *stubPaymentClient) CreatePaymentPayload(
	context.Context,
	types.PaymentRequirements,
	*types.ResourceInfo,
	map[string]interface{},
) (types.PaymentPayload, error) {
	c.createCalls.Add(1)
	return types.PaymentPayload{
		X402Version: 2,
		Payload: map[string]interface{}{
			"signature": "0xsigned-by-wallet",
		},
	}, nil
}

type recordingSpendingStore struct {
	mu     sync.Mutex
	wallet string
	amount decimal.Decimal
}

func (s *recordingSpendingStore) GetDailySpent(context.Context, string, string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (s *recordingSpendingStore) ReserveSpend(
	_ context.Context,
	wallet string,
	_ string,
	amount decimal.Decimal,
	maxTotal decimal.Decimal,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxTotal.IsPositive() && s.amount.Add(amount).GreaterThan(maxTotal) {
		return fmt.Errorf("reservation exceeds test budget")
	}
	s.wallet = wallet
	s.amount = s.amount.Add(amount)
	return nil
}

func TestX402PaymentHttpClientRetriesWithStandardPaymentHeader(t *testing.T) {
	required := testPaymentRequired(t)
	requiredRaw, err := json.Marshal(required)
	if err != nil {
		t.Fatal(err)
	}

	var attempts atomic.Int32
	var replayedBody atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			t.Errorf("read request body: %v", readErr)
		}
		if attempts.Add(1) == 1 {
			w.Header().Set(HeaderPaymentRequired, base64.StdEncoding.EncodeToString(requiredRaw))
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}

		replayedBody.Store(bytes.Equal(body, []byte(`{"query":"weather"}`)))
		if req.Header.Get("X402-Authorization") != "" {
			t.Error("legacy X402-Authorization header must not be sent")
		}
		encoded := req.Header.Get("PAYMENT-SIGNATURE")
		if encoded == "" {
			t.Error("PAYMENT-SIGNATURE header is required")
		} else {
			payloadRaw, decodeErr := base64.StdEncoding.DecodeString(encoded)
			if decodeErr != nil {
				t.Errorf("decode payment signature header: %v", decodeErr)
			}
			var payload types.PaymentPayload
			if decodeErr := json.Unmarshal(payloadRaw, &payload); decodeErr != nil {
				t.Errorf("decode payment payload: %v", decodeErr)
			} else if payload.X402Version != 2 {
				t.Errorf("unexpected payload version: %d", payload.X402Version)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	paymentClient := &stubPaymentClient{}
	spendingStore := &recordingSpendingStore{}
	policyEngine := policy.NewEngine(&config.PoliciesConfig{MaxDailyBudgetUSD: 1}, spendingStore)
	headerEncoder := x402http.Newx402HTTPClient(x402.Newx402Client())
	client, err := NewX402PaymentHttpClient(X402Config{
		PayerAddress: "0x1111111111111111111111111111111111111111",
	}, policyEngine, paymentClient, headerEncoder, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Post(context.Background(), server.URL, []byte(`{"query":"weather"}`), nil)
	if err != nil {
		t.Fatalf("paid request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected retry status: %d", resp.StatusCode)
	}
	if attempts.Load() != 2 || !replayedBody.Load() {
		t.Fatalf("expected one body-preserving retry, attempts=%d replayed=%v", attempts.Load(), replayedBody.Load())
	}
	if paymentClient.createCalls.Load() != 1 {
		t.Fatalf("expected one signed payload, got %d", paymentClient.createCalls.Load())
	}
	spendingStore.mu.Lock()
	defer spendingStore.mu.Unlock()
	if spendingStore.wallet != "0x1111111111111111111111111111111111111111" || !spendingStore.amount.Equal(decimal.RequireFromString("0.01")) {
		t.Fatalf("unexpected budget reservation: wallet=%s amount=%s", spendingStore.wallet, spendingStore.amount)
	}
}

type nonReplayableReader struct {
	reader io.Reader
}

func (r nonReplayableReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func TestX402PaymentHttpClientRejectsNonReplayableBodyBeforeSigning(t *testing.T) {
	requiredRaw, err := json.Marshal(testPaymentRequired(t))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(HeaderPaymentRequired, base64.StdEncoding.EncodeToString(requiredRaw))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer server.Close()

	paymentClient := &stubPaymentClient{}
	client, err := NewX402PaymentHttpClient(
		X402Config{PayerAddress: "0x1111111111111111111111111111111111111111"},
		policy.NewEngine(&config.PoliciesConfig{}, nil),
		paymentClient,
		x402http.Newx402HTTPClient(x402.Newx402Client()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL, nonReplayableReader{reader: strings.NewReader("payload")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req); err == nil || !strings.Contains(err.Error(), "not replayable") {
		t.Fatalf("expected non-replayable body error, got %v", err)
	}
	if paymentClient.createCalls.Load() != 0 {
		t.Fatal("Wallet signing must not occur for a non-replayable request")
	}
}
