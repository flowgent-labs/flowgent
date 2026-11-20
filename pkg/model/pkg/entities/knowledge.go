package entities

// KnowledgeEntry is the API projection of a versioned knowledge document and
// one content hit. It is not a persistence catch-all: stores map it onto the
// source -> document -> revision -> content -> embedding hierarchy.
type KnowledgeEntry struct {
	BaseEntity

	DocumentRevisionID string         `json:"document_revision_id,omitempty" db:"-"`
	Revision           int64          `json:"revision,omitempty" db:"-"`
	ContentID          string         `json:"content_id,omitempty" db:"-"`
	ParentContentID    string         `json:"parent_content_id,omitempty" db:"-"`
	Title              string         `json:"title" db:"-"`
	Content            string         `json:"content" db:"-"`
	ContentType        string         `json:"content_type" db:"-"`
	ContentKind        string         `json:"content_kind,omitempty" db:"-"`
	Source             string         `json:"source" db:"-"`
	SourceRef          string         `json:"source_ref" db:"-"`
	SourceRevision     string         `json:"source_revision,omitempty" db:"-"`
	Scope              string         `json:"scope" db:"-"`
	FlowID             string         `json:"flow_id,omitempty" db:"-"`
	FlowName           string         `json:"flow_name,omitempty" db:"-"`
	RunID              string         `json:"run_id,omitempty" db:"-"`
	ACLRef             string         `json:"acl_ref,omitempty" db:"-"`
	Classification     string         `json:"classification,omitempty" db:"-"`
	HeadingPath        string         `json:"heading_path,omitempty" db:"-"`
	PageStart          *int           `json:"page_start,omitempty" db:"-"`
	PageEnd            *int           `json:"page_end,omitempty" db:"-"`
	Provenance         map[string]any `json:"provenance,omitempty" db:"-"`
	Tags               []string       `json:"tags" db:"-"`
	Embedding          []float32      `json:"embedding,omitempty" db:"-"`
	ProfileKey         string         `json:"profile_key,omitempty" db:"-"`
	Distance           *float64       `json:"distance,omitempty" db:"-"`
}

// KnowledgeSource describes an ingestion boundary. Credentials belong in
// referenced runtime configuration, never in Config as plaintext.
type KnowledgeSource struct {
	BaseEntity
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

type KnowledgeDocument struct {
	BaseEntity
	SourceID          string `json:"source_id"`
	DocumentKey       string `json:"document_key"`
	Scope             string `json:"scope"`
	FlowID            string `json:"flow_id,omitempty"`
	FlowName          string `json:"flow_name,omitempty" db:"-"`
	RunID             string `json:"run_id,omitempty"`
	ExternalID        string `json:"external_id,omitempty"`
	SourceURI         string `json:"source_uri,omitempty"`
	ACLRef            string `json:"acl_ref,omitempty"`
	Classification    string `json:"classification"`
	CurrentRevisionID string `json:"current_revision_id,omitempty"`
}

type KnowledgeDocumentRevision struct {
	BaseEntity
	DocumentID     string         `json:"document_id"`
	Revision       int64          `json:"revision"`
	SourceRevision string         `json:"source_revision,omitempty"`
	Title          string         `json:"title"`
	SourcePath     string         `json:"source_path,omitempty"`
	Provenance     map[string]any `json:"provenance"`
	ContentHash    string         `json:"content_hash"`
	ApprovalID     string         `json:"approval_id,omitempty"`
}

type KnowledgeContent struct {
	BaseEntity
	DocumentRevisionID string `json:"document_revision_id"`
	ParentID           string `json:"parent_id,omitempty"`
	Type               string `json:"type"`
	Ordinal            int    `json:"ordinal"`
	HeadingPath        string `json:"heading_path,omitempty"`
	PageStart          *int   `json:"page_start,omitempty"`
	PageEnd            *int   `json:"page_end,omitempty"`
	Content            string `json:"content"`
	ContentHash        string `json:"content_hash"`
}

type EmbeddingProfile struct {
	BaseEntity
	ProfileKey     string `json:"profile_key"`
	ProviderType   string `json:"provider_type"`
	Model          string `json:"model"`
	ModelRevision  string `json:"model_revision"`
	Dimensions     int    `json:"dimensions"`
	DistanceMetric string `json:"distance_metric"`
}

type KnowledgeEmbedding struct {
	BaseEntity
	ContentID      string    `json:"content_id"`
	ProfileKey     string    `json:"profile_key"`
	Dimensions     int       `json:"dimensions"`
	DistanceMetric string    `json:"distance_metric"`
	InputHash      string    `json:"input_hash"`
	Embedding      []float32 `json:"embedding" db:"-"`
}

type KnowledgeCandidate struct {
	BaseEntity
	SourceRunID      string         `json:"source_run_id"`
	TargetScope      string         `json:"target_scope"`
	TargetFlowID     string         `json:"target_flow_id,omitempty"`
	TargetFlowName   string         `json:"target_flow_name,omitempty" db:"-"`
	TargetDocumentID string         `json:"target_document_id,omitempty"`
	Type             string         `json:"type"`
	Content          string         `json:"content"`
	ContentHash      string         `json:"content_hash"`
	Provenance       map[string]any `json:"provenance"`
	ExpectedRevision int64          `json:"expected_revision"`
	ApprovalID       string         `json:"approval_id"`
	IdempotencyKey   string         `json:"idempotency_key"`
}

// KnowledgeSearchRequest is the input for semantic or keyword knowledge search.
type KnowledgeSearchRequest struct {
	Query      string    `json:"query"`
	Embedding  []float32 `json:"embedding,omitempty"`
	ProfileKey string    `json:"profile_key,omitempty"`
	TopK       int       `json:"top_k"`
	Tags       []string  `json:"tags,omitempty"`
	Scope      string    `json:"scope,omitempty"`
	FlowID     string    `json:"flow_id,omitempty"`
	FlowName   string    `json:"flow_name,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
}
