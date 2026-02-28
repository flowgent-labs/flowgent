package x402

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/types"
)

func TestParse_V2Body(t *testing.T) {
	pr := types.PaymentRequired{
		X402Version: 2,
		Accepts: []types.PaymentRequirements{{
			Scheme: "x402", Network: "base", Asset: "USDC",
			Amount: "0.01", PayTo: "0x1234",
		}},
	}
	body, _ := json.Marshal(pr)

	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	parsed, err := Parse(resp)
	if err != nil {
		t.Fatalf("Parse V2 body: %v", err)
	}
	if len(parsed.Accepts) != 1 {
		t.Fatalf("expected 1 accept, got %d", len(parsed.Accepts))
	}
	accept := parsed.Accepts[0]
	if accept.Asset != "USDC" {
		t.Errorf("expected USDC, got %s", accept.Asset)
	}
	if accept.Amount != "0.01" {
		t.Errorf("expected 0.01, got %s", accept.Amount)
	}
}

func TestParse_Not402(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	_, err := Parse(resp)
	if err == nil {
		t.Fatal("expected error for non-402 response")
	}
}

func TestParse_V1HeaderFallback(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header: http.Header{
			"X402-Payment": []string{`{"asset":"USDC","amount":"0.05","chain":"base","recipient":"0x1234","settlement":"x402","facilitator":"http://f.example.com"}`},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(`not valid json`))),
	}

	parsed, err := Parse(resp)
	if err != nil {
		t.Fatalf("Parse V1 fallback: %v", err)
	}
	accept := parsed.Accepts[0]
	if accept.Asset != "USDC" {
		t.Errorf("expected USDC, got %s", accept.Asset)
	}
}

func TestParse_EmptyResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader([]byte{})),
	}
	_, err := Parse(resp)
	if err == nil {
		t.Fatal("expected error for empty 402 response")
	}
}

func TestIsX402Response(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusPaymentRequired}
	if !IsX402Response(resp) {
		t.Fatal("should detect x402 response by 402 status")
	}
	resp2 := &http.Response{StatusCode: http.StatusOK}
	if IsX402Response(resp2) {
		t.Fatal("should NOT detect x402 on 200")
	}
}

func TestSetAuthorizationHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	SetAuthorizationHeader(req, "tok-123")
	if req.Header.Get(HeaderX402Auth) != "tok-123" {
		t.Error("header should be set")
	}
}

func TestFirstAccept(t *testing.T) {
	pr := &types.PaymentRequired{
		Accepts: []types.PaymentRequirements{
			{Scheme: "exact", Asset: "USDC"},
			{Scheme: "permit2", Asset: "ETH"},
		},
	}
	first := FirstAccept(pr)
	if first.Asset != "USDC" {
		t.Errorf("expected USDC, got %s", first.Asset)
	}

	if a := FirstAccept(&types.PaymentRequired{}); a != nil {
		t.Error("expected nil for empty accepts")
	}
	if a := FirstAccept(nil); a != nil {
		t.Error("expected nil for nil")
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

var _ = httptest.NewServer
