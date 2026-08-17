package entities

import "time"

// FlowRelease is an immutable, portable snapshot published by one namespace.
// SHARED means visible inside this Flowgent deployment; PRIVATE releases are
// visible only to the producer and namespaces with an explicit grant.
type FlowRelease struct {
	BaseEntity
	FlowID         string    `json:"flow_id"`
	FlowVersion    int64     `json:"flow_version"`
	ReleaseVersion string    `json:"release_version"`
	Definition     FlowInfo  `json:"definition"`
	Checksum       string    `json:"checksum"`
	Visibility     string    `json:"visibility"`
	PublishedAt    time.Time `json:"published_at"`
}

// FlowReleaseGrant allows one consumer namespace to discover and install a
// PRIVATE release. The grant never transfers ownership or secrets.
type FlowReleaseGrant struct {
	BaseEntity
	ReleaseID         string     `json:"release_id"`
	ConsumerNamespace string     `json:"consumer_namespace"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

// FlowInstallation records the immutable release provenance of a consumer's
// local flow copy. Runtime inputs, secrets, runs and traces remain consumer
// namespace data and are never shared with the producer.
type FlowInstallation struct {
	BaseEntity
	ReleaseID         string            `json:"release_id"`
	ReleaseVersion    string            `json:"release_version"`
	ProducerNamespace string            `json:"producer_namespace"`
	InstalledFlowID   string            `json:"installed_flow_id"`
	ReleaseChecksum   string            `json:"release_checksum"`
	AppliedChecksum   string            `json:"applied_checksum"`
	ResourceBindings  map[string]string `json:"resource_bindings"`
	InstalledAt       time.Time         `json:"installed_at"`
}
