package x402

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/flowgent-labs/flowgent/src/payments"
)

func TestParse_Valid(t *testing.T) {
	pr := payments.X402PaymentRequest{
		Asset: "USDC", Amount: decimal.NewFromFloat(0.01),
		Chain: "base", Recipient: "0x1234",
		Settlement: "x402", Facilitator: "https://f.example.com",
	}
	headerVal, _ := json.Marshal(pr)

	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{HeaderX402Payment: []string{string(headerVal)}},
	}

	parsed, err := Parse(resp)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Asset != "USDC" {
		t.Errorf("expected USDC, got %s", parsed.Asset)
	}
	if !parsed.Amount.Equals(decimal.NewFromFloat(0.01)) {
		t.Errorf("expected 0.01, got %s", parsed.Amount)
	}
}

func TestParse_Not402(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	_, err := Parse(resp)
	if err == nil {
		t.Fatal("expected error for non-402 response")
	}
}

func TestParse_MissingHeader(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{},
	}
	_, err := Parse(resp)
	if err == nil {
		t.Fatal("expected error for missing X402-Payment header")
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{HeaderX402Payment: []string{"not-json"}},
	}
	_, err := Parse(resp)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIsX402Response(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{HeaderX402Payment: []string{`{"asset":"USDC","amount":"0.01","chain":"base","recipient":"0x","settlement":"x402","facilitator":"http://f"}`}},
	}
	if !IsX402Response(resp) {
		t.Fatal("should detect x402 response")
	}
	resp2 := &http.Response{StatusCode: http.StatusOK}
	if IsX402Response(resp2) {
		t.Fatal("should NOT detect x402 on 200")
	}
	resp3 := &http.Response{StatusCode: http.StatusPaymentRequired, Header: http.Header{}}
	if IsX402Response(resp3) {
		t.Fatal("should NOT detect x402 without header")
	}
}

func TestParseFromResponse(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	pr, err := ParseFromResponse(resp)
	if err != nil {
		t.Fatalf("ParseFromResponse: %v", err)
	}
	if pr != nil {
		t.Fatal("should return nil for non-402")
	}
}

func TestSetAuthorizationHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	SetAuthorizationHeader(req, "tok-123")
	if req.Header.Get(HeaderX402Auth) != "tok-123" {
		t.Error("header should be set")
	}
}

func TestFormatPaymentHeader(t *testing.T) {
	pr := &payments.X402PaymentRequest{
		Asset: "USDC", Amount: decimal.NewFromFloat(0.1),
		Chain: "base", Recipient: "0x", Facilitator: "http://f",
	}
	s, err := FormatPaymentHeader(pr)
	if err != nil {
		t.Fatalf("FormatPaymentHeader: %v", err)
	}
	var back payments.X402PaymentRequest
	if err := json.Unmarshal([]byte(s), &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.Asset != "USDC" {
		t.Error("round-trip failed")
	}
}

func TestParseAssetAmount(t *testing.T) {
	d, err := ParseAssetAmount(" 0.05 ")
	if err != nil {
		t.Fatalf("ParseAssetAmount: %v", err)
	}
	if !d.Equals(decimal.NewFromFloat(0.05)) {
		t.Errorf("expected 0.05, got %s", d)
	}
}

// Ensure httptest is used for future HTTP server tests
var _ = httptest.NewServer
