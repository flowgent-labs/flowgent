package model

import "context"

// SignClient sends unsigned payment payloads for signing.
// In distributed mode, signing is delegated via MQTT to the wallet daemon.
// In local mode, signing is performed in-process.
type SignClient interface {
	Sign(ctx context.Context, walletAddr string, payload []byte) ([]byte, error)
}
