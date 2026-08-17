package authz

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/google/uuid"
)

type Repository interface {
	ListPrincipalGroups(context.Context, string, string) ([]string, error)
	FindBindings(context.Context, string, entities.PrincipalType, string, []string) ([]*entities.IAMRoleBinding, error)
	ListRoles(context.Context, string) ([]*entities.IAMRole, error)
	GetAPIKey(context.Context, string) (*entities.IAMAPIKey, error)
	SaveAPIKey(context.Context, *entities.IAMAPIKey) error
	GetPrincipal(context.Context, string) (*entities.IAMPrincipal, error)
	FindPrincipalByIdentity(context.Context, string, string) (*entities.IAMPrincipal, error)
	AppendAudit(context.Context, *entities.IAMAuditEvent) error
}

type AccessRequest struct {
	PrincipalID       string
	PrincipalType     entities.PrincipalType
	ExternalGroups    []string
	DirectPermissions []string
	AllowedNamespaces []string
	Namespace         string
	Permission        string
	ResourceType      string
	ResourceID        string
	SourceIP          string
	RequestID         string
	Now               time.Time
}

type Decision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	RoleID  string `json:"role_id,omitempty"`
}

type Service struct {
	cfg  config.AuthorizationConfig
	repo Repository
}

// AuthorizeAdditional evaluates and audits a handler-level permission whose
// target is discovered only after decoding the request body (for example the
// Resource Pool selected by a Flow definition). Primary route authorization
// remains in Middleware; this method composes a second, independently scoped
// decision without coupling handlers to role storage.
func (s *Service) AuthorizeAdditional(r *http.Request, namespace, permission, resourceType, resourceID string) (bool, error) {
	if !s.Enabled() {
		return true, nil
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return false, nil
	}
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.NewString()
		r.Header.Set("X-Request-ID", requestID)
	}
	policy := RoutePolicy{Namespace: namespace, Permission: permission, ResourceType: resourceType, ResourceID: resourceID}
	decision, err := s.Authorize(r.Context(), AccessRequest{
		PrincipalID: user.UserID, PrincipalType: user.Type, ExternalGroups: user.Groups,
		DirectPermissions: user.DirectPermissions, AllowedNamespaces: user.AllowedNamespaces,
		Namespace: namespace, Permission: permission, ResourceType: resourceType,
		ResourceID: resourceID, SourceIP: sourceIP(r), RequestID: requestID,
	})
	if err != nil {
		s.audit(r.Context(), user, policy, Decision{Reason: "policy_backend_error"}, requestID, sourceIP(r))
		return false, err
	}
	s.audit(r.Context(), user, policy, decision, requestID, sourceIP(r))
	return decision.Allowed, nil
}

func NewService(cfg config.AuthorizationConfig, repo Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && !strings.EqualFold(s.cfg.Enforcement, "disabled")
}

// AuthenticateCredential validates break-glass, component, and dynamic API-key
// credentials. Comparisons are constant-time and plaintext credentials are
// never persisted or logged.
func (s *Service) AuthenticateCredential(ctx context.Context, raw string) (*auth.UserInfo, error) {
	if s == nil || raw == "" {
		return nil, errors.New("credential not recognized")
	}
	if constantTimeEqual(raw, configuredCredential(s.cfg.BootstrapToken)) {
		return &auth.UserInfo{UserID: "breakglass:root", Issuer: "flowgent:breakglass", Username: "breakglass-root", Type: entities.PrincipalUser}, nil
	}
	for name, token := range s.cfg.InternalTokens {
		if token = configuredCredential(token); token != "" && constantTimeEqual(raw, token) {
			return &auth.UserInfo{UserID: "service:" + name, Issuer: "flowgent:internal", Username: name, Type: entities.PrincipalServiceAccount}, nil
		}
	}
	if !strings.HasPrefix(raw, "fgk_") {
		return nil, errors.New("credential not recognized")
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, "fgk_"), "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("invalid API key")
	}
	key, err := s.repo.GetAPIKey(ctx, parts[0])
	if err != nil || key == nil || key.DelFlag || key.Status != "ACTIVE" || key.RevokedAt != nil {
		return nil, errors.New("invalid API key")
	}
	now := time.Now().UTC()
	if key.ExpiresAt != nil && !key.ExpiresAt.After(now) {
		return nil, errors.New("API key expired")
	}
	digest := apiKeyDigest(parts[0], parts[1])
	stored, err := hex.DecodeString(key.SecretHash)
	if err != nil || subtle.ConstantTimeCompare(digest[:], stored) != 1 {
		return nil, errors.New("invalid API key")
	}
	key.LastUsedAt = &now
	key.MarkUpdated(key.PrincipalID)
	if err := s.repo.SaveAPIKey(ctx, key); err != nil {
		slog.Warn("authz: update API key last-used failed", "key_id", key.ID, "error", err)
	}
	principal, err := s.repo.GetPrincipal(ctx, key.PrincipalID)
	if err != nil || principal == nil || principal.Status != "ACTIVE" || principal.DelFlag {
		return nil, errors.New("API key principal is inactive")
	}
	return &auth.UserInfo{
		UserID:            principal.ID,
		Issuer:            principal.Issuer,
		Username:          principal.Username,
		Email:             principal.Email,
		DisplayName:       principal.DisplayName,
		Type:              principal.Type,
		DirectPermissions: append([]string(nil), key.Permissions...),
		AllowedNamespaces: append([]string(nil), key.AllowedNamespaces...),
		CredentialID:      key.ID,
	}, nil
}

func configuredCredential(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "${") || strings.Contains(value, "{{") {
		return ""
	}
	return value
}

func (s *Service) Authorize(ctx context.Context, req AccessRequest) (Decision, error) {
	if !s.Enabled() {
		return Decision{Allowed: true, Reason: "authorization_disabled"}, nil
	}
	if req.PrincipalID == "" {
		return Decision{Reason: "unauthenticated"}, nil
	}
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if len(req.AllowedNamespaces) > 0 && !matchesNamespace(req.AllowedNamespaces, req.Namespace) {
		return Decision{Reason: "credential_namespace_attenuation"}, nil
	}
	if len(req.DirectPermissions) > 0 && !anyPermissionMatches(req.DirectPermissions, req.Permission) {
		return Decision{Reason: "credential_permission_attenuation"}, nil
	}
	groups, err := s.repo.ListPrincipalGroups(ctx, req.Namespace, req.PrincipalID)
	if err != nil {
		return Decision{}, fmt.Errorf("resolve principal groups: %w", err)
	}
	groups = uniqueStrings(append(groups, req.ExternalGroups...))
	bindings, err := s.repo.FindBindings(ctx, req.Namespace, req.PrincipalType, req.PrincipalID, groups)
	if err != nil {
		return Decision{}, fmt.Errorf("resolve role bindings: %w", err)
	}
	roles, err := s.repo.ListRoles(ctx, req.Namespace)
	if err != nil {
		return Decision{}, fmt.Errorf("resolve roles: %w", err)
	}
	roleByID := make(map[string]*entities.IAMRole, len(roles))
	for _, role := range roles {
		roleByID[role.ID] = role
	}
	var allow *Decision
	for _, binding := range bindings {
		role := roleByID[binding.RoleID]
		if role == nil || role.DelFlag || role.Status != "ACTIVE" || !anyPermissionMatches(role.Permissions, req.Permission) {
			continue
		}
		if !bindingMatchesScope(binding, req) || !bindingMatchesConditions(binding, req) {
			continue
		}
		if strings.EqualFold(binding.Effect, "DENY") {
			return Decision{Reason: "explicit_deny", RoleID: role.ID}, nil
		}
		decision := Decision{Allowed: true, Reason: "role_binding", RoleID: role.ID}
		allow = &decision
	}
	if allow != nil {
		return *allow, nil
	}
	return Decision{Reason: "no_matching_role_binding"}, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		policy, known := PolicyForRequest(r)
		if !known {
			writeForbidden(w, "protected API route has no authorization policy")
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeForbidden(w, "authenticated principal is required")
			return
		}
		// OIDC/LDAP tokens carry the upstream immutable subject. Resolve it to
		// the deployment principal ID used by role bindings. Unprovisioned
		// identities remain unmatched and are denied by default.
		if principal, resolveErr := s.repo.FindPrincipalByIdentity(r.Context(), user.Issuer, user.UserID); resolveErr == nil && principal != nil && principal.Status == "ACTIVE" && !principal.DelFlag {
			user.UserID = principal.ID
		}
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		decision, err := s.Authorize(r.Context(), AccessRequest{
			PrincipalID: user.UserID, PrincipalType: user.Type, ExternalGroups: user.Groups,
			DirectPermissions: user.DirectPermissions, AllowedNamespaces: user.AllowedNamespaces,
			Namespace: policy.Namespace, Permission: policy.Permission,
			ResourceType: policy.ResourceType, ResourceID: policy.ResourceID,
			SourceIP: sourceIP(r), RequestID: requestID,
		})
		if err != nil {
			s.audit(r.Context(), user, policy, Decision{Reason: "policy_backend_error"}, requestID, sourceIP(r))
			http.Error(w, "authorization service unavailable", http.StatusServiceUnavailable)
			return
		}
		s.audit(r.Context(), user, policy, decision, requestID, sourceIP(r))
		if !decision.Allowed && !strings.EqualFold(s.cfg.Enforcement, "audit") {
			writeForbidden(w, "access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) audit(ctx context.Context, user *auth.UserInfo, policy RoutePolicy, decision Decision, requestID, ip string) {
	if (decision.Allowed && !s.cfg.AuditAllow) || (!decision.Allowed && !s.cfg.AuditDeny) {
		return
	}
	now := time.Now().UTC()
	result := "DENY"
	if decision.Allowed {
		result = "ALLOW"
	}
	event := &entities.IAMAuditEvent{
		BaseEntity: entities.BaseEntity{ID: uuid.NewString(), Namespace: policy.Namespace},
		ActorID:    user.UserID, ActorType: user.Type, Action: policy.Permission,
		ResourceType: policy.ResourceType, ResourceID: policy.ResourceID,
		Decision: result, Reason: decision.Reason, RequestID: requestID, SourceIP: ip,
		Metadata: map[string]any{"role_id": decision.RoleID},
	}
	event.MarkCreated(user.UserID)
	event.CreatedAt, event.UpdatedAt = now, now
	// Authorization audit evidence must survive a client disconnect or browser
	// navigation after the decision has already been made. Detach cancellation,
	// but retain a strict deadline so an unavailable audit store cannot stall API
	// traffic indefinitely.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.repo.AppendAudit(auditCtx, event); err != nil {
		slog.Error("authz: append audit event failed", "request_id", requestID, "error", err)
	}
}

func apiKeyDigest(id, secret string) [sha256.Size]byte {
	return sha256.Sum256([]byte(id + "\x00" + secret))
}

// GenerateAPIKey returns the persistent ID, one-time plaintext credential, and
// hexadecimal digest. Callers must return Raw once and persist only Hash.
func GenerateAPIKey() (id, raw, hash, prefix, suffix string, err error) {
	id = uuid.NewString()
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", "", "", "", fmt.Errorf("generate API key: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	raw = "fgk_" + id + "_" + secret
	digest := apiKeyDigest(id, secret)
	hash = hex.EncodeToString(digest[:])
	prefix = "fgk_" + id[:8]
	suffix = secret[len(secret)-4:]
	return
}

func constantTimeEqual(left, right string) bool {
	if left == "" || right == "" || len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func matchesNamespace(allowed []string, namespace string) bool {
	for _, candidate := range allowed {
		if candidate == "*" || candidate == namespace {
			return true
		}
	}
	return false
}

func bindingMatchesScope(binding *entities.IAMRoleBinding, req AccessRequest) bool {
	if binding.ExpiresAt != nil && !binding.ExpiresAt.After(req.Now) {
		return false
	}
	if binding.Namespace != "*" && binding.Namespace != req.Namespace {
		return false
	}
	switch binding.ResourceType {
	case "platform":
		return binding.Namespace == "*"
	case "namespace", "*":
		return true
	default:
		return binding.ResourceType == req.ResourceType && (binding.ResourceID == "*" || binding.ResourceID == req.ResourceID)
	}
}

func bindingMatchesConditions(binding *entities.IAMRoleBinding, req AccessRequest) bool {
	if len(binding.Conditions) == 0 {
		return true
	}
	if values, ok := stringSlice(binding.Conditions["source_cidrs"]); ok && len(values) > 0 {
		ip := net.ParseIP(req.SourceIP)
		if ip == nil {
			return false
		}
		matched := false
		for _, raw := range values {
			_, network, err := net.ParseCIDR(raw)
			if err == nil && network.Contains(ip) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func stringSlice(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		values, ok := value.([]string)
		return values, ok
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		values = append(values, text)
	}
	return values, true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeForbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"success":false,"message":%q}`, message)
}
