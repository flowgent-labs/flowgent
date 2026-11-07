// Package secretbox provides versioned authenticated encryption for secrets
// that are persisted outside a dedicated secret manager.
package secretbox

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

const envelopeVersion = 1

// Envelope is safe to persist or transmit. Plaintext and encryption keys are
// never included in it.
type Envelope struct {
	Version    int    `json:"version"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ISecretCipher isolates storage-facing code from the concrete key provider.
// Implementations must provide authenticated encryption and bind aad.
type ISecretCipher interface {
	Seal(ctx context.Context, plaintext, aad []byte) (*Envelope, error)
	Open(ctx context.Context, envelope *Envelope, aad []byte) ([]byte, error)
}

// AESGCMSecretCipher is the default local-key implementation. A keyring rather
// than one key supports online rotation: new writes use activeKeyID while old
// envelopes remain readable until their key is retired.
type AESGCMSecretCipher struct {
	activeKeyID string
	keys        map[string][]byte
}

// NewAESGCMSecretCipher builds a cipher from base64-encoded AES-256 keys.
func NewAESGCMSecretCipher(activeKeyID string, encodedKeys map[string]string) (*AESGCMSecretCipher, error) {
	if activeKeyID == "" {
		return nil, fmt.Errorf("secretbox: active_key_id is required")
	}
	keys := make(map[string][]byte, len(encodedKeys))
	for id, encoded := range encodedKeys {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secretbox: decode key %q: %w", id, err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("secretbox: key %q must decode to 32 bytes", id)
		}
		keys[id] = key
	}
	if _, ok := keys[activeKeyID]; !ok {
		return nil, fmt.Errorf("secretbox: active key %q is not in keyring", activeKeyID)
	}
	return &AESGCMSecretCipher{activeKeyID: activeKeyID, keys: keys}, nil
}

func (c *AESGCMSecretCipher) Seal(_ context.Context, plaintext, aad []byte) (*Envelope, error) {
	aead, err := c.aead(c.activeKeyID)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secretbox: nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	return &Envelope{
		Version: envelopeVersion, Algorithm: "AES-256-GCM", KeyID: c.activeKeyID,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}, nil
}

func (c *AESGCMSecretCipher) Open(_ context.Context, envelope *Envelope, aad []byte) ([]byte, error) {
	if envelope == nil || envelope.Version != envelopeVersion || envelope.Algorithm != "AES-256-GCM" {
		return nil, fmt.Errorf("secretbox: unsupported envelope")
	}
	aead, err := c.aead(envelope.KeyID)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("secretbox: invalid nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("secretbox: invalid ciphertext: %w", err)
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("secretbox: authentication failed: %w", err)
	}
	return plaintext, nil
}

func (c *AESGCMSecretCipher) aead(keyID string) (cipher.AEAD, error) {
	key, ok := c.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("secretbox: key %q is unavailable", keyID)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: AES: %w", err)
	}
	return cipher.NewGCM(block)
}

// SealJSON and OpenJSON keep canonical JSON handling in one place.
func SealJSON(ctx context.Context, cipher ISecretCipher, value any, aad []byte) (*Envelope, error) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("secretbox: marshal: %w", err)
	}
	return cipher.Seal(ctx, plaintext, aad)
}

func OpenJSON(ctx context.Context, cipher ISecretCipher, envelope *Envelope, aad []byte, target any) error {
	plaintext, err := cipher.Open(ctx, envelope, aad)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(plaintext, target); err != nil {
		return fmt.Errorf("secretbox: unmarshal: %w", err)
	}
	return nil
}
