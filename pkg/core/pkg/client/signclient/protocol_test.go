//go:build x402

package signclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

type transportFunc func(*SignRequest) (*SignResponse, error)

func (function transportFunc) RoundTrip(_ context.Context, request *SignRequest) (*SignResponse, error) {
	return function(request)
}

func TestDigestClientVerifiesRecoveredAddress(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000007")
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	transport := transportFunc(func(request *SignRequest) (*SignResponse, error) {
		digest, decodeErr := base64.StdEncoding.DecodeString(request.DigestB64)
		if decodeErr != nil {
			return nil, decodeErr
		}
		signature, signErr := crypto.Sign(digest, privateKey)
		if signErr != nil {
			return nil, signErr
		}
		signature[64] += 27
		return successResponse(request, address, fmt.Sprintf("0x%x", signature)), nil
	})
	client, err := NewDigestClient(transport, "flowgent-test", "payer", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	digest := crypto.Keccak256([]byte("payment authorization"))
	signature, err := client.SignDigest(context.Background(), "x402.payment", digest)
	if err != nil {
		t.Fatalf("SignDigest: %v", err)
	}
	if len(signature) != 65 {
		t.Fatalf("signature length = %d, want 65", len(signature))
	}
}

func TestDigestClientRejectsSignatureFromAnotherKey(t *testing.T) {
	expectedKey, _ := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000007")
	otherKey, _ := crypto.HexToECDSA("0000000000000000000000000000000000000000000000000000000000000008")
	address := crypto.PubkeyToAddress(expectedKey.PublicKey).Hex()
	transport := transportFunc(func(request *SignRequest) (*SignResponse, error) {
		digest, _ := base64.StdEncoding.DecodeString(request.DigestB64)
		signature, _ := crypto.Sign(digest, otherKey)
		signature[64] += 27
		return successResponse(request, address, fmt.Sprintf("0x%x", signature)), nil
	})
	client, err := NewDigestClient(transport, "flowgent-test", "payer", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.SignDigest(context.Background(), "x402.payment", crypto.Keccak256([]byte("payment authorization")))
	if err == nil {
		t.Fatal("expected an address-recovery error")
	}
}

func TestDigestClientRejectsMismatchedResponseIdentity(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	transport := transportFunc(func(request *SignRequest) (*SignResponse, error) {
		response := successResponse(request, address, "0x"+fmt.Sprintf("%0130s", ""))
		response.RequestID = "another-request"
		return response, nil
	})
	client, err := NewDigestClient(transport, "flowgent-test", "payer", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.SignDigest(context.Background(), "x402.payment", make([]byte, 32))
	if err == nil {
		t.Fatal("expected a mismatched-response error")
	}
}

func successResponse(request *SignRequest, address, signature string) *SignResponse {
	return &SignResponse{
		Version:   ProtocolVersion,
		RequestID: request.RequestID,
		ClientID:  request.ClientID,
		WalletID:  request.WalletID,
		Address:   address,
		Signature: signature,
		SignedAt:  time.Now().Unix(),
	}
}
