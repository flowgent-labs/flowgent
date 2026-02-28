package facilitator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	x402 "github.com/x402-foundation/x402/go"
	"github.com/x402-foundation/x402/go/types"
)

func TestClient_Health_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestClient_Health_NoEndpoint(t *testing.T) {
	client := New("", 5*time.Second)
	if err := client.Health(context.Background()); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestClient_Health_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	if err := client.Health(context.Background()); err == nil {
		t.Fatal("expected error for unhealthy facilitator")
	}
}

func TestClient_Supported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(x402.SupportedResponse{
			Kinds: []x402.SupportedKind{{X402Version: 2, Scheme: "exact", Network: "eip155:84532"}},
		})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	result, err := client.Supported(context.Background())
	if err != nil {
		t.Fatalf("Supported: %v", err)
	}
	if len(result.Kinds) == 0 {
		t.Error("expected kinds in supported response")
	}
}

func TestClient_Verify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/verify" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(x402.VerifyResponse{IsValid: true})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	vr, err := client.Verify(context.Background(), &types.PaymentRequirements{
		Scheme: "exact", Network: "eip155:84532",
		PayTo: "0x1234", Amount: "0.01", Asset: "USDC",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !vr.IsValid {
		t.Error("expected valid verification")
	}
}

func TestClient_Verify_Invalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(x402.VerifyResponse{
			IsValid: false, InvalidReason: "insufficient_funds", InvalidMessage: "not enough balance",
		})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	vr, err := client.Verify(context.Background(), &types.PaymentRequirements{
		Scheme: "exact", Network: "eip155:84532",
		PayTo: "0x1234", Amount: "1000.0", Asset: "USDC",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if vr.IsValid {
		t.Error("expected invalid verification")
	}
}

func TestClient_Authorize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/settle" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(x402.SettleResponse{
			Success: true, Transaction: "0xtx123", Network: "eip155:84532",
		})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	payload := &types.PaymentPayload{
		X402Version: 2,
		Payload:     map[string]interface{}{"intent_id": "int-1"},
		Accepted: types.PaymentRequirements{
			Scheme: "exact", Network: "eip155:84532",
			PayTo: "0xwallet", Amount: "0.01", Asset: "USDC",
		},
	}

	receipt, err := client.Authorize(context.Background(), payload)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.TxHash != "0xtx123" {
		t.Errorf("expected tx 0xtx123, got %s", receipt.TxHash)
	}
}

func TestClient_Authorize_NoEndpoint(t *testing.T) {
	client := New("", 5*time.Second)
	_, err := client.Authorize(context.Background(), &types.PaymentPayload{})
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestClient_Authorize_FacilitatorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	_, err := client.Authorize(context.Background(), &types.PaymentPayload{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

var _ = httptest.NewServer
