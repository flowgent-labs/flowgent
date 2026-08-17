package entities

import "time"

// PrincipalType identifies a human, group, or non-human workload identity.
type PrincipalType string

const (
	PrincipalUser           PrincipalType = "user"
	PrincipalGroup          PrincipalType = "group"
	PrincipalServiceAccount PrincipalType = "service_account"
)

// IAMNamespace is the GitHub-Organization-equivalent business/team boundary.
// A deployment is the implicit enterprise; no Enterprise or Workspace layer
// is introduced between the deployment and this namespace.
type IAMNamespace struct {
	BaseEntity
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
}

// IAMPrincipal is a deployment-scoped identity. Namespace membership and
// permissions are expressed through groups and role bindings, not embedded in
// authentication tokens.
type IAMPrincipal struct {
	BaseEntity
	Type        PrincipalType  `json:"type"`
	Issuer      string         `json:"issuer"`
	ExternalID  string         `json:"external_id"`
	Username    string         `json:"username"`
	DisplayName string         `json:"display_name"`
	Email       string         `json:"email,omitempty"`
	Attributes  map[string]any `json:"attributes"`
}

// IAMNamespaceMember is the explicit organization membership edge between a
// deployment identity and one namespace. Identity lifecycle stays global;
// removing a member only revokes that namespace's groups and grants.
type IAMNamespaceMember struct {
	BaseEntity
	PrincipalID string `json:"principal_id"`
	Membership  string `json:"membership"`
}

// IAMGroup is a namespace-owned team. External directory group names can be
// bound directly, while this entity supports Flowgent-managed teams.
type IAMGroup struct {
	BaseEntity
	Name string `json:"name"`
}

// IAMGroupMember connects one principal to a namespace-owned group.
type IAMGroupMember struct {
	BaseEntity
	GroupID     string `json:"group_id"`
	PrincipalID string `json:"principal_id"`
}

// IAMRole is a reusable permission set. Built-in roles use namespace_id="*";
// custom roles belong to exactly one namespace.
type IAMRole struct {
	BaseEntity
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
}

// IAMRoleBinding grants or explicitly denies a role to a subject at platform,
// namespace, or exact-resource scope. Explicit DENY always takes precedence.
type IAMRoleBinding struct {
	BaseEntity
	RoleID       string         `json:"role_id"`
	SubjectType  PrincipalType  `json:"subject_type"`
	SubjectID    string         `json:"subject_id"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	Effect       string         `json:"effect"`
	Conditions   map[string]any `json:"conditions"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
}

// IAMAPIKey is a revocable, attenuated machine credential. SecretHash is never
// serialized; only the prefix and suffix are returned after creation.
type IAMAPIKey struct {
	BaseEntity
	Name              string     `json:"name"`
	PrincipalID       string     `json:"principal_id"`
	SecretHash        string     `json:"-" db:"secret_hash"`
	Prefix            string     `json:"prefix"`
	Suffix            string     `json:"suffix"`
	AllowedNamespaces []string   `json:"namespaces" db:"allowed_namespaces"`
	Permissions       []string   `json:"permissions"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

// IAMAuditEvent is an append-only security decision and administration record.
// Request and correlation IDs allow an external SIEM to reconstruct a chain.
type IAMAuditEvent struct {
	BaseEntity
	ActorID      string         `json:"actor_id"`
	ActorType    PrincipalType  `json:"actor_type"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	Decision     string         `json:"decision"`
	Reason       string         `json:"reason"`
	RequestID    string         `json:"request_id"`
	SourceIP     string         `json:"source_ip"`
	Metadata     map[string]any `json:"metadata"`
}

// IAMBindingView is the evaluator's denormalized role-binding record.
type IAMBindingView struct {
	Binding    IAMRoleBinding `json:"binding"`
	RoleName   string         `json:"role_name"`
	Permission []string       `json:"permissions"`
}
