package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const (
	postgresVectorMaxDimensions = 16000
	sqliteVecMaxDimensions      = 8192
)

func normalizePage(page entities.PageRequest) entities.PageRequest {
	if page.Page < 1 {
		page.Page = 1
	}
	if page.Size < 1 {
		page.Size = 20
	}
	if page.Size > 200 {
		page.Size = 200
	}
	return page
}

func normalizedTopK(value int) int {
	if value <= 0 {
		return 20
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeKnowledgeScope(entry *entities.KnowledgeEntry) (string, string, string, error) {
	graphKey := strings.TrimSpace(entry.AgentGraphName)
	if graphKey == "" {
		graphKey = strings.TrimSpace(entry.AgentGraphID)
	}
	scope := strings.ToLower(strings.TrimSpace(entry.Scope))
	if scope == "" && entry.Metadata != nil {
		scope, _ = entry.Metadata["scope"].(string)
		scope = strings.ToLower(strings.TrimSpace(scope))
	}
	if scope == "" {
		scope = "namespace"
	}
	switch scope {
	case "namespace":
		if graphKey != "" || entry.GraphRunID != "" {
			return "", "", "", fmt.Errorf("namespace knowledge cannot reference agent_graph_id or graph_run_id")
		}
		return scope, "", "", nil
	case "graph":
		if graphKey == "" || entry.GraphRunID != "" {
			return "", "", "", fmt.Errorf("graph knowledge requires agent_graph_id and forbids graph_run_id")
		}
		return scope, graphKey, "", nil
	case "run":
		if graphKey == "" || entry.GraphRunID == "" {
			return "", "", "", fmt.Errorf("run knowledge requires agent_graph_id and graph_run_id")
		}
		return scope, graphKey, entry.GraphRunID, nil
	default:
		return "", "", "", fmt.Errorf("invalid knowledge scope %q", entry.Scope)
	}
}

func normalizeContentKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "section":
		return "section"
	case "summary":
		return "summary"
	default:
		return "chunk"
	}
}

func knowledgeMetadata(entry *entities.KnowledgeEntry) map[string]any {
	metadata := make(map[string]any, len(entry.Metadata)+2)
	for key, value := range entry.Metadata {
		metadata[key] = value
	}
	metadata["tags"] = append([]string(nil), entry.Tags...)
	metadata["content_type"] = defaultString(entry.ContentType, "text/plain")
	return metadata
}

func candidateDocumentTitle(candidate *entities.KnowledgeCandidate) string {
	if candidate != nil && candidate.Metadata != nil {
		if title, ok := candidate.Metadata["title"].(string); ok && strings.TrimSpace(title) != "" {
			return strings.TrimSpace(title)
		}
	}
	if candidate != nil && strings.TrimSpace(candidate.Description) != "" {
		return strings.TrimSpace(candidate.Description)
	}
	if candidate == nil {
		return "Run summary"
	}
	return "Run " + candidate.SourceGraphRunID + " summary"
}

func stringSlice(value any) []string {
	switch list := value.(type) {
	case []string:
		return append([]string(nil), list...)
	case []any:
		result := make([]string, 0, len(list))
		for _, item := range list {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return []string{}
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validateEmbeddingProfile(profile *entities.EmbeddingProfile) error {
	if profile == nil {
		return fmt.Errorf("embedding profile is required")
	}
	profile.Namespace = defaultString(strings.TrimSpace(profile.Namespace), "default")
	profile.ProfileKey = strings.TrimSpace(profile.ProfileKey)
	profile.ProviderType = strings.ToLower(strings.TrimSpace(profile.ProviderType))
	profile.DistanceMetric = strings.ToLower(strings.TrimSpace(profile.DistanceMetric))
	if profile.ProfileKey == "" || profile.ProviderType == "" || strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(profile.ModelRevision) == "" {
		return fmt.Errorf("profile_key, provider_type, model, and model_revision are required")
	}
	if profile.Dimensions <= 0 {
		return fmt.Errorf("embedding dimensions must be positive")
	}
	switch profile.DistanceMetric {
	case "cosine", "l2", "l1":
	default:
		return fmt.Errorf("unsupported distance metric %q", profile.DistanceMetric)
	}
	return nil
}

func sqliteVectorTable(namespace, profileKey string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + profileKey))
	return "knw_vec_" + hex.EncodeToString(sum[:12])
}
