package entities

// KnowledgeEntry is a persisted knowledge base entry with vector-ready embedding support.
type KnowledgeEntry struct {
	BaseEntity

	Title       string         `json:"title" db:"title"`
	Content     string         `json:"content" db:"content"`
	ContentType string         `json:"content_type" db:"content_type"`
	Source      string         `json:"source" db:"source"`
	SourceRef   string         `json:"source_ref" db:"source_ref"`
	Tags        []string       `json:"tags" db:"tags"`
	Embedding   []float32      `json:"embedding,omitempty" db:"-"` // not persisted via generic store
	Metadata    map[string]any `json:"metadata" db:"metadata"`
}

// KnowledgeSearchRequest is the input for semantic or keyword knowledge search.
type KnowledgeSearchRequest struct {
	Query string   `json:"query"`
	TopK  int      `json:"top_k"`
	Tags  []string `json:"tags,omitempty"`
}
