//go:build x402

package signclient

import (
	"context"
	"fmt"

	"github.com/x402-foundation/x402/go/mechanisms/evm"
)

type RemoteEVMSigner struct {
	client *DigestClient
}

func NewRemoteEVMSigner(client *DigestClient) (*RemoteEVMSigner, error) {
	if client == nil {
		return nil, fmt.Errorf("wallet digest client is required")
	}
	return &RemoteEVMSigner{client: client}, nil
}

func (s *RemoteEVMSigner) Address() string { return s.client.Address() }

func (s *RemoteEVMSigner) SignTypedData(
	ctx context.Context,
	domain evm.TypedDataDomain,
	types map[string][]evm.TypedDataField,
	primaryType string,
	message map[string]interface{},
) ([]byte, error) {
	digest, err := evm.HashTypedData(domain, types, primaryType, message)
	if err != nil {
		return nil, fmt.Errorf("hash EIP-712 payment authorization: %w", err)
	}
	return s.client.SignDigest(ctx, "x402.payment", digest)
}

var _ evm.ClientEvmSigner = (*RemoteEVMSigner)(nil)
