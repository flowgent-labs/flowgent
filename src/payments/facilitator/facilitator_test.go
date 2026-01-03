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

func TestClient_Authorize_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/authorize" {
			t.Errorf("expected /authorize, got %s", r.URL.Path)
		}
		receipt := payments.PaymentReceipt{
			ID: "rec-1", IntentID: "int-1", TxHash: "0xtx",
			Asset: "USDC", Authorization: "tok-123",
			Facilitator: "test", PaidAt: time.Now(),
		}
		json.NewEncoder(w).Encode(receipt)
	}))
	defer server.Close()

	client := New(server.URL, 5*time.Second)
	auth := &payments.PaymentAuthorization{
		IntentID: "int-1", Wallet: "0x1234",
		Signature: "sig", Payload: "int-1:0.01:USDC:0xrecv",
	}

	receipt, err := client.Authorize(context.Background(), auth)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if receipt.IntentID != "int-1" {
		t.Errorf("expected int-1, got %s", receipt.IntentID)
	}
	if receipt.ID != "rec-1" {
		t.Errorf("expected rec-1, got %s", receipt.ID)
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

func TestClient_Health(t *testing.T) {
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

	client2 := New("", 5*time.Second)
	if err := client2.Health(context.Background()); err == nil {
		t.Fatal("expected error for empty endpoint health check")
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
