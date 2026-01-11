package facilitator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/payments"
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

func TestClient_SupportedNetworks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"kinds": []map[string]any{{"network": "eip155:84532", "scheme": "exact"}},
		})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	result, err := client.SupportedNetworks(context.Background())
	if err != nil {
		t.Fatalf("SupportedNetworks: %v", err)
	}
	if kinds, ok := result["kinds"]; !ok || kinds == nil {
		t.Error("expected kinds in supported response")
	}
}

func TestClient_Verify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/verify" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(VerifyResponse{IsValid: true})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	vr, err := client.Verify(context.Background(), &VerifyRequest{
		Scheme: "exact", Network: "eip155:84532",
		Recipient: "0x1234", Amount: "0.01", Asset: "USDC",
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
		json.NewEncoder(w).Encode(VerifyResponse{
			IsValid: false, Reason: "insufficient_funds", Message: "not enough balance",
		})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	vr, err := client.Verify(context.Background(), &VerifyRequest{
		Scheme: "exact", Network: "eip155:84532",
		Recipient: "0x1234", Amount: "1000.0", Asset: "USDC",
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
		json.NewEncoder(w).Encode(SettleResponse{TxHash: "0xtx123", Status: "confirmed"})
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	auth := &payments.PaymentAuthorization{
		IntentID: "int-1", Wallet: "0xwallet",
		Signature: "sig-data", Payload: "payload",
	}

	receipt, err := client.Authorize(context.Background(), auth)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.IntentID != "int-1" {
		t.Errorf("expected int-1, got %s", receipt.IntentID)
	}
	if receipt.TxHash != "0xtx123" {
		t.Errorf("expected tx 0xtx123, got %s", receipt.TxHash)
	}
}

func TestClient_Authorize_NoEndpoint(t *testing.T) {
	client := New("", 5*time.Second)
	_, err := client.Authorize(context.Background(), &payments.PaymentAuthorization{})
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
	_, err := client.Authorize(context.Background(), &payments.PaymentAuthorization{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

// Ensure httptest is used
var _ = httptest.NewServer
