package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

const (
	authGuardContextHeader       = "x-authguard-context"
	authGuardContextPrefix       = "agctx1"
	authGuardContextVersion      = 3
	authGuardMinimumHMACKeyBytes = 32
)

type authGuardAccessContext struct {
	Version           int      `json:"version"`
	PrincipalID       string   `json:"principal_id"`
	Action            string   `json:"action"`
	ResourceURN       string   `json:"resource_urn"`
	AllowResourceURNs []string `json:"allow_resource_urns"`
	DenyResourceURNs  []string `json:"deny_resource_urns"`
	IssuedAtEpochSec  uint64   `json:"issued_at_epoch_seconds"`
	ExpiresAtEpochSec uint64   `json:"expires_at_epoch_seconds"`
}

func authGuardSQLScope(access authGuardAccessContext) storepkg.FlowgentSqlScope {
	const namespaceMarker = ":namespace/"
	marker := strings.Index(access.ResourceURN, namespaceMarker)
	if marker < 0 {
		return storepkg.DenyFlowgentSqlScope()
	}
	resourcePath := access.ResourceURN[marker+len(namespaceMarker):]
	namespace, _, _ := strings.Cut(resourcePath, "/")
	return storepkg.NamespaceFlowgentSqlScope(namespace)
}

func authenticateAuthGuardContext(raw, signingKey string, now time.Time) (*UserInfo, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, false, nil
	}
	if len([]byte(signingKey)) < authGuardMinimumHMACKeyBytes {
		return nil, true, fmt.Errorf("AuthGuard HMAC key must contain at least %d bytes", authGuardMinimumHMACKeyBytes)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != authGuardContextPrefix || parts[1] == "" || parts[2] == "" {
		return nil, true, errors.New("invalid signed access context format")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, true, errors.New("invalid signed access context signature")
	}
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, true, errors.New("invalid signed access context signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, true, fmt.Errorf("decode signed access context: %w", err)
	}
	var access authGuardAccessContext
	if err := json.Unmarshal(payload, &access); err != nil {
		return nil, true, fmt.Errorf("decode signed access context: %w", err)
	}
	if access.Version != authGuardContextVersion {
		return nil, true, fmt.Errorf("unsupported access context version: %d", access.Version)
	}
	if access.PrincipalID == "" || access.Action == "" || access.ResourceURN == "" {
		return nil, true, errors.New("incomplete signed access context")
	}
	nowSec := uint64(now.Unix())
	if access.ExpiresAtEpochSec <= access.IssuedAtEpochSec || access.ExpiresAtEpochSec <= nowSec {
		return nil, true, errors.New("expired signed access context")
	}
	if access.IssuedAtEpochSec > nowSec+30 {
		return nil, true, errors.New("signed access context issued in the future")
	}
	sqlScope := authGuardSQLScope(access)
	return &UserInfo{
		UserID:   access.PrincipalID,
		Issuer:   "authguard",
		Username: access.PrincipalID,
		Type:     entities.PrincipalUser,
		Extra: map[string]any{
			"authguard_action":       access.Action,
			"authguard_resource_urn": access.ResourceURN,
		},
		flowgentSqlScope: &sqlScope,
	}, true, nil
}
