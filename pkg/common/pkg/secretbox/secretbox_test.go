package secretbox

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestAESGCMSecretCipherRoundTripAndAAD(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := NewAESGCMSecretCipher("v1", map[string]string{"v1": key})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := cipher.Seal(context.Background(), []byte(`{"token":"secret"}`), []byte("tenant|channel"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(envelope.Ciphertext, "secret") {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := cipher.Open(context.Background(), envelope, []byte("tenant|channel"))
	if err != nil || string(plaintext) != `{"token":"secret"}` {
		t.Fatalf("Open() = %q, %v", plaintext, err)
	}
	if _, err := cipher.Open(context.Background(), envelope, []byte("other")); err == nil {
		t.Fatal("Open() accepted different AAD")
	}
}

func TestAESGCMSecretCipherRejectsInvalidKey(t *testing.T) {
	_, err := NewAESGCMSecretCipher("v1", map[string]string{"v1": base64.StdEncoding.EncodeToString([]byte("short"))})
	if err == nil {
		t.Fatal("expected invalid key error")
	}
}
