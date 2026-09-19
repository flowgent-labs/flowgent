package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

const authGuardTestKey = "flowgent-authguard-test-hmac-key-32-bytes"

func TestAuthenticateAuthGuardContext(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	valid := signedAuthGuardContext(t, authGuardAccessContext{
		Version: authGuardContextVersion, PrincipalID: "principal-security-owner",
		Action: "flowgent.flow.read", ResourceURN: "urn:iam:prod:flowgent:global:acme:namespace/security/flow/fixer",
		IssuedAtEpochSec: uint64(now.Unix()), ExpiresAtEpochSec: uint64(now.Add(time.Minute).Unix()),
	})

	user, present, err := authenticateAuthGuardContext(valid, authGuardTestKey, now)
	if err != nil || !present || user == nil || user.UserID != "principal-security-owner" {
		t.Fatalf("valid context = (%#v, %v, %v)", user, present, err)
	}

	tampered := valid[:len(valid)-1] + "A"
	if _, present, err = authenticateAuthGuardContext(tampered, authGuardTestKey, now); err == nil || !present {
		t.Fatalf("tampered context must fail closed: present=%v err=%v", present, err)
	}

	expired := signedAuthGuardContext(t, authGuardAccessContext{
		Version: authGuardContextVersion, PrincipalID: "principal-security-owner",
		Action: "flowgent.flow.read", ResourceURN: "urn:iam:prod:flowgent:global:acme:namespace/security/flow/fixer",
		IssuedAtEpochSec:  uint64(now.Add(-2 * time.Minute).Unix()),
		ExpiresAtEpochSec: uint64(now.Add(-time.Minute).Unix()),
	})
	if _, _, err = authenticateAuthGuardContext(expired, authGuardTestKey, now); err == nil {
		t.Fatal("expired context must fail closed")
	}

	if user, present, err = authenticateAuthGuardContext("", authGuardTestKey, now); err != nil || present || user != nil {
		t.Fatalf("absent context = (%#v, %v, %v)", user, present, err)
	}
}

func signedAuthGuardContext(t *testing.T, context authGuardAccessContext) string {
	t.Helper()
	payload, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	input := authGuardContextPrefix + "." + encoded
	mac := hmac.New(sha256.New, []byte(authGuardTestKey))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
