package entities

import "time"

// FlowRelease is an immutable, portable snapshot published by one namespace.
// SHARED means visible inside this Flowgent deployment; PRIVATE releases are
// visible only to the producer and namespaces with an explicit grant.
type FlowRelease struct {
	BaseEntity
	FlowID         string    `json:"flow_id"`
	FlowName       string    `json:"flow_name" db:"-"`
	FlowRevisionID string    `json:"flow_revision_id"`
	FlowRevision   int64     `json:"flow_revision"`
	FlowVersion    int64     `json:"-" db:"-"` // deprecated runtime alias
	ReleaseVersion string    `json:"release_version"`
	Definition     FlowInfo  `json:"definition"`
	Checksum       string    `json:"checksum"`
	Visibility     string    `json:"visibility"`
	PublishedAt    time.Time `json:"published_at"`
}

func (r *FlowRelease) NormalizeAliases() {
	if r.FlowRevision == 0 {
		r.FlowRevision = r.FlowVersion
	}
	if r.FlowVersion == 0 {
		r.FlowVersion = r.FlowRevision
	}
}

// FlowReleaseGrant allows one consumer namespace to discover and install a
// PRIVATE release. The grant never transfers ownership or secrets.
type FlowReleaseGrant struct {
	BaseEntity
	ReleaseID           string     `json:"release_id"`
	ConsumerNamespaceID string     `json:"consumer_namespace_id"`
	ConsumerNamespace   string     `json:"-" db:"-"` // deprecated runtime alias
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
}

func (g *FlowReleaseGrant) NormalizeAliases() {
	if g.ConsumerNamespaceID == "" {
		g.ConsumerNamespaceID = g.ConsumerNamespace
	}
	if g.ConsumerNamespace == "" {
		g.ConsumerNamespace = g.ConsumerNamespaceID
	}
}

// FlowInstallation records the immutable release provenance of a consumer's
// local flow copy. Runtime inputs, secrets, runs and traces remain consumer
// namespace data and are never shared with the producer.
type FlowInstallation struct {
	BaseEntity
	ReleaseID           string            `json:"release_id"`
	ReleaseVersion      string            `json:"release_version"`
	ProducerNamespaceID string            `json:"producer_namespace_id"`
	ProducerNamespace   string            `json:"-" db:"-"` // deprecated runtime alias
	InstalledFlowID     string            `json:"installed_flow_id"`
	InstalledFlowName   string            `json:"installed_flow_name" db:"-"`
	ReleaseChecksum     string            `json:"release_checksum"`
	AppliedChecksum     string            `json:"applied_checksum"`
	ResourceBindings    map[string]string `json:"resource_bindings"`
	InstalledAt         time.Time         `json:"installed_at"`
}

func (i *FlowInstallation) NormalizeAliases() {
	if i.ProducerNamespaceID == "" {
		i.ProducerNamespaceID = i.ProducerNamespace
	}
	if i.ProducerNamespace == "" {
		i.ProducerNamespace = i.ProducerNamespaceID
	}
}
