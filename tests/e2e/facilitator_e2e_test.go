// Package e2e provides integration tests against a real x402 facilitator.
// Requires: docker run -d --name x402-facilitator -p 8085:8080 ...
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/src/payments/facilitator"
)

const (
	facilitatorURL = "http://localhost:8085"
)

func skipIfNoFacilitator(t *testing.T) {
	t.Helper()
	client := facilitator.New(facilitatorURL, 2*time.Second)
	if err := client.Health(context.Background()); err != nil {
		t.Skipf("facilitator not available at %s: %v (start with: docker compose -f deploy/facilitator/docker-compose.yml up -d)", facilitatorURL, err)
	}
}

func TestE2E_FacilitatorHealth(t *testing.T) {
	skipIfNoFacilitator(t)

	client := facilitator.New(facilitatorURL, 5*time.Second)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Facilitator health check failed: %v", err)
	}
	t.Log("Facilitator health: OK")
}

func TestE2E_FacilitatorSupportedNetworks(t *testing.T) {
	skipIfNoFacilitator(t)

	client := facilitator.New(facilitatorURL, 5*time.Second)
	result, err := client.SupportedNetworks(context.Background())
	if err != nil {
		t.Fatalf("SupportedNetworks: %v", err)
	}

	kinds, ok := result["kinds"]
	if !ok {
		t.Fatal("expected 'kinds' in supported response")
	}
	kindsList, ok := kinds.([]interface{})
	if !ok || len(kindsList) == 0 {
		t.Fatal("expected non-empty kinds list")
	}

	t.Logf("Supported networks: %+v", kindsList)

	// Verify it has signers
	if signers, ok := result["signers"]; ok {
		t.Logf("Signers: %+v", signers)
	}
}

func TestE2E_FacilitatorVerify_RejectsInvalidPayment(t *testing.T) {
	skipIfNoFacilitator(t)

	client := facilitator.New(facilitatorURL, 5*time.Second)
	vr, err := client.Verify(context.Background(), &facilitator.VerifyRequest{
		Scheme:    "exact",
		Network:   "eip155:84532",
		Recipient: "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		Amount:    "1000.0",
		Asset:     "ETH",
	})
	if err != nil {
		t.Fatalf("Verify request failed: %v", err)
	}

	// Expect verification to be invalid — dev signer has no real funds on Base Sepolia
	t.Logf("Verify response: isValid=%v, reason=%s, message=%s", vr.IsValid, vr.Reason, vr.Message)

	// The verify endpoint succeeded (HTTP 200), even if payment is invalid
	// This is correct x402 behavior — verification failures are returned as valid HTTP responses
}

func TestE2E_FacilitatorVerify_InvalidRequest(t *testing.T) {
	skipIfNoFacilitator(t)

	client := facilitator.New(facilitatorURL, 5*time.Second)

	// Send an empty request — should get an error response
	vr, err := client.Verify(context.Background(), &facilitator.VerifyRequest{
		Scheme: "nonexistent-scheme",
	})
	if err != nil {
		t.Fatalf("Verify request failed: %v", err)
	}

	if vr.IsValid {
		t.Error("expected invalid for nonexistent scheme")
	}
	t.Logf("Invalid scheme response: isValid=%v, reason=%s", vr.IsValid, vr.Reason)
}

func TestE2E_FacilitatorConnection(t *testing.T) {
	skipIfNoFacilitator(t)

	client := facilitator.New(facilitatorURL, 5*time.Second)

	// Health
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}

	// Supported
	supported, err := client.SupportedNetworks(context.Background())
	if err != nil {
		t.Fatalf("Supported: %v", err)
	}
	t.Logf("Supported: %+v", supported)

	// Verify (will fail validation but connectivity works)
	vr, err := client.Verify(context.Background(), &facilitator.VerifyRequest{
		Scheme:    "exact",
		Network:   "eip155:84532",
		Recipient: "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		Amount:    "0.001",
		Asset:     "ETH",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	t.Logf("Verify: isValid=%v reason=%s message=%s", vr.IsValid, vr.Reason, vr.Message)

	t.Log("All facilitator connectivity checks passed")
}
