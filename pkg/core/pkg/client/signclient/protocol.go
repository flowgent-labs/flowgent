//go:build x402

package signclient

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/x402-foundation/x402/go/mechanisms/evm"
)

const (
	ProtocolVersion            = "wallet.sign.v1"
	EIP712Secp256k1            = "eip712-secp256k1"
	DefaultRequestTopicPrefix  = "wallet/v1/sign/requests"
	DefaultResponseTopicPrefix = "wallet/v1/sign/responses"
	maxRequestTTL              = 30 * time.Second
)

type SignRequest struct {
	Version   string            `json:"version"`
	RequestID string            `json:"request_id"`
	ClientID  string            `json:"client_id"`
	WalletID  string            `json:"wallet_id"`
	Purpose   string            `json:"purpose"`
	Scheme    string            `json:"scheme"`
	DigestB64 string            `json:"digest_b64"`
	IssuedAt  int64             `json:"issued_at"`
	ExpiresAt int64             `json:"expires_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type SignResponse struct {
	Version   string         `json:"version"`
	RequestID string         `json:"request_id"`
	ClientID  string         `json:"client_id"`
	WalletID  string         `json:"wallet_id"`
	Address   string         `json:"address,omitempty"`
	Signature string         `json:"signature,omitempty"`
	SignedAt  int64          `json:"signed_at,omitempty"`
	Error     *ProtocolError `json:"error,omitempty"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Transport interface {
	RoundTrip(ctx context.Context, request *SignRequest) (*SignResponse, error)
}

type DigestClient struct {
	transport Transport
	clientID  string
	walletID  string
	address   string
	timeout   time.Duration
}

func NewDigestClient(transport Transport, clientID, walletID, address string, timeout time.Duration) (*DigestClient, error) {
	if transport == nil {
		return nil, fmt.Errorf("wallet transport is required")
	}
	if err := ValidateIdentity(clientID, walletID, address); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &DigestClient{
		transport: transport,
		clientID:  clientID,
		walletID:  walletID,
		address:   address,
		timeout:   timeout,
	}, nil
}

// ValidateIdentity checks the public client-to-Wallet contract before a
// transport opens network resources.
func ValidateIdentity(clientID, walletID, address string) error {
	if err := validateIdentifier("client_id", clientID); err != nil {
		return err
	}
	if err := validateIdentifier("wallet_id", walletID); err != nil {
		return err
	}
	if !isEOAAddress(address) {
		return fmt.Errorf("wallet public_address must be a 20-byte 0x-prefixed EOA address")
	}
	return nil
}

func (c *DigestClient) Address() string { return c.address }

func (c *DigestClient) SignDigest(ctx context.Context, purpose string, digest []byte) ([]byte, error) {
	if len(digest) != 32 {
		return nil, fmt.Errorf("wallet signing requires a 32-byte EIP-712 digest")
	}
	now := time.Now()
	ttl := c.timeout
	if ttl > maxRequestTTL {
		ttl = maxRequestTTL
	}
	if ttl < time.Second {
		ttl = time.Second
	}
	request := &SignRequest{
		Version:   ProtocolVersion,
		RequestID: uuid.NewString(),
		ClientID:  c.clientID,
		WalletID:  c.walletID,
		Purpose:   purpose,
		Scheme:    EIP712Secp256k1,
		DigestB64: base64.StdEncoding.EncodeToString(digest),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}
	response, err := c.transport.RoundTrip(ctx, request)
	if err != nil {
		return nil, err
	}
	if response.Version != ProtocolVersion || response.RequestID != request.RequestID || response.ClientID != c.clientID || response.WalletID != c.walletID {
		return nil, fmt.Errorf("wallet returned a mismatched signing response")
	}
	if response.Error != nil {
		return nil, fmt.Errorf("wallet %s: %s", response.Error.Code, response.Error.Message)
	}
	if !strings.EqualFold(response.Address, c.address) {
		return nil, fmt.Errorf("wallet returned unexpected address %q", response.Address)
	}
	signatureHex := strings.TrimPrefix(response.Signature, "0x")
	signature, err := hex.DecodeString(signatureHex)
	if err != nil || len(signature) != 65 || (signature[64] != 27 && signature[64] != 28) {
		return nil, fmt.Errorf("wallet returned an invalid recoverable EOA signature")
	}
	valid, err := evm.VerifyEOASignature(digest, signature, common.HexToAddress(c.address))
	if err != nil || !valid {
		return nil, fmt.Errorf("wallet signature does not recover to the configured EOA address")
	}
	return signature, nil
}

func validateIdentifier(field, value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("%s is invalid", field)
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-:", char) {
			continue
		}
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func isEOAAddress(address string) bool {
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return false
	}
	_, err := hex.DecodeString(address[2:])
	return err == nil
}
