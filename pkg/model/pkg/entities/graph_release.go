package entities

import "time"

// AgentGraphRelease is an immutable, portable snapshot published by one namespace.
// SHARED means visible inside this Flowgent deployment; PRIVATE releases are
// visible only to the producer and namespaces with an explicit grant.
type AgentGraphRelease struct {
	BaseEntity
	AgentGraphID         string     `json:"agent_graph_id"`
	AgentGraphName       string     `json:"agent_graph_name" db:"-"`
	AgentGraphRevisionID string     `json:"agent_graph_revision_id"`
	AgentGraphRevision   int64      `json:"agent_graph_revision"`
	ReleaseVersion       string     `json:"release_version"`
	Definition           AgentGraph `json:"definition"`
	Checksum             string     `json:"checksum"`
	Visibility           string     `json:"visibility"`
	PublishedAt          time.Time  `json:"published_at"`
}

// AgentGraphReleaseGrant allows one consumer namespace to discover and install a
// PRIVATE release. The grant never transfers ownership or secrets.
type AgentGraphReleaseGrant struct {
	BaseEntity
	ReleaseID           string     `json:"release_id"`
	ConsumerNamespaceID string     `json:"consumer_namespace_id"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
}

// AgentGraphInstallation records the immutable release provenance of a consumer's
// local graph copy. Runtime inputs, secrets, runs and traces remain consumer
// namespace data and are never shared with the producer.
type AgentGraphInstallation struct {
	BaseEntity
	ReleaseID               string            `json:"release_id"`
	ReleaseVersion          string            `json:"release_version"`
	ProducerNamespaceID     string            `json:"producer_namespace_id"`
	InstalledAgentGraphID   string            `json:"installed_agent_graph_id"`
	InstalledAgentGraphName string            `json:"installed_agent_graph_name" db:"-"`
	ReleaseChecksum         string            `json:"release_checksum"`
	AppliedChecksum         string            `json:"applied_checksum"`
	ResourceBindings        map[string]string `json:"resource_bindings"`
	InstalledAt             time.Time         `json:"installed_at"`
}
