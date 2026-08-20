//go:build x402

package client

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/x402-foundation/x402/go/types"
)

func testPaymentRequired(t *testing.T) types.PaymentRequired {
	t.Helper()
	return types.PaymentRequired{
		X402Version: 2,
		Accepts: []types.PaymentRequirements{{
			Scheme: "exact", Network: "eip155:8453",
			Asset:  "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
			Amount: "10000", PayTo: "0x1234",
		}},
	}
}

func TestParseV2Header(t *testing.T) {
	raw, err := json.Marshal(testPaymentRequired(t))
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header: http.Header{
			http.CanonicalHeaderKey(HeaderPaymentRequired): []string{base64.StdEncoding.EncodeToString(raw)},
		},
	}

	parsed, err := Parse(resp)
	if err != nil {
		t.Fatalf("Parse V2 header: %v", err)
	}
	if len(parsed.Accepts) != 1 || parsed.Accepts[0].Amount != "10000" {
		t.Fatalf("unexpected payment requirements: %+v", parsed.Accepts)
	}
}

func TestParseRejectsInvalidResponses(t *testing.T) {
	versionOne, err := json.Marshal(types.PaymentRequired{X402Version: 1, Accepts: []types.PaymentRequirements{{}}})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]*http.Response{
		"nil":     nil,
		"not 402": {StatusCode: http.StatusOK},
		"missing header": {
			StatusCode: http.StatusPaymentRequired,
			Header:     make(http.Header),
		},
		"unsupported version": {
			StatusCode: http.StatusPaymentRequired,
			Header: http.Header{
				http.CanonicalHeaderKey(HeaderPaymentRequired): []string{base64.StdEncoding.EncodeToString(versionOne)},
			},
		},
	}
	for name, resp := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(resp); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestIsX402Response(t *testing.T) {
	if !IsX402Response(&http.Response{StatusCode: http.StatusPaymentRequired}) {
		t.Fatal("should detect x402 response by status")
	}
	if IsX402Response(&http.Response{StatusCode: http.StatusOK}) || IsX402Response(nil) {
		t.Fatal("should reject non-x402 responses")
	}
}

func TestParsePaymentAmountUSD(t *testing.T) {
	requirement := testPaymentRequired(t).Accepts[0]
	requirement.Amount = "50000"
	amount, err := ParsePaymentAmountUSD(requirement)
	if err != nil {
		t.Fatalf("ParsePaymentAmountUSD: %v", err)
	}
	if !amount.Equals(decimal.NewFromFloat(0.05)) {
		t.Fatalf("expected 0.05, got %s", amount)
	}
	for _, invalid := range []string{"", "invalid", "0", "-1", "0.5"} {
		requirement.Amount = invalid
		if _, err := ParsePaymentAmountUSD(requirement); err == nil {
			t.Fatalf("expected amount %q to be rejected", invalid)
		}
	}
	requirement.Amount = "50000"
	requirement.Asset = "0x1111111111111111111111111111111111111111"
	if _, err := ParsePaymentAmountUSD(requirement); err == nil {
		t.Fatal("expected unknown token to be rejected")
	}
}
