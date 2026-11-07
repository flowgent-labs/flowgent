package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func candidateType(value string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "knowledge":
		return "knowledge", "knowledge_publish", nil
	case "instruction":
		return "instruction", "instruction_publish", nil
	default:
		return "", "", fmt.Errorf("candidate type must be knowledge or instruction")
	}
}

func candidateRequest(candidate *entities.KnowledgeCandidate) map[string]any {
	return map[string]any{
		"candidate_id":       candidate.ID,
		"source_run_id":      candidate.SourceRunID,
		"target_scope":       candidate.TargetScope,
		"target_flow_id":     nilIfEmpty(candidate.TargetFlowID),
		"target_document_id": nilIfEmpty(candidate.TargetDocumentID),
		"type":               candidate.Type,
		"content":            candidate.Content,
		"content_hash":       candidate.ContentHash,
		"expected_revision":  candidate.ExpectedRevision,
		"provenance":         candidate.Provenance,
	}
}

func canonicalHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func candidatePrincipal(candidate *entities.KnowledgeCandidate) string {
	if candidate.CreatedBy != "" {
		return candidate.CreatedBy
	}
	if candidate.UpdatedBy != "" {
		return candidate.UpdatedBy
	}
	return "system:flowgent"
}

func validateCandidate(candidate *entities.KnowledgeCandidate) error {
	if candidate == nil {
		return fmt.Errorf("candidate is required")
	}
	candidate.Namespace = defaultString(strings.TrimSpace(candidate.Namespace), "default")
	candidate.SourceRunID = strings.TrimSpace(candidate.SourceRunID)
	candidate.TargetScope = strings.ToLower(strings.TrimSpace(candidate.TargetScope))
	candidate.Type = strings.ToLower(strings.TrimSpace(candidate.Type))
	candidate.Content = strings.TrimSpace(candidate.Content)
	if candidate.SourceRunID == "" || candidate.Content == "" {
		return fmt.Errorf("source_run_id and content are required")
	}
	if candidate.TargetScope != "namespace" && candidate.TargetScope != "flow" {
		return fmt.Errorf("target_scope must be namespace or flow")
	}
	if _, _, err := candidateType(candidate.Type); err != nil {
		return err
	}
	if candidate.Type == "instruction" && candidate.TargetDocumentID != "" {
		return fmt.Errorf("instruction candidate cannot target a knowledge document")
	}
	if candidate.ExpectedRevision < 0 {
		return fmt.Errorf("expected_revision cannot be negative")
	}
	if candidate.IdempotencyKey == "" {
		candidate.IdempotencyKey = "summary:" + candidate.SourceRunID + ":" + canonicalHash(map[string]any{
			"scope": candidate.TargetScope, "flow": candidate.TargetFlowID,
			"document": candidate.TargetDocumentID, "type": candidate.Type,
			"content": candidate.Content,
		})
	}
	candidate.ContentHash = hashText(candidate.Content)
	if candidate.Provenance == nil {
		candidate.Provenance = map[string]any{}
	}
	return nil
}

func candidateMatches(existing, requested *entities.KnowledgeCandidate) bool {
	return existing.SourceRunID == requested.SourceRunID &&
		existing.TargetScope == requested.TargetScope &&
		existing.TargetFlowID == requested.TargetFlowID &&
		existing.TargetDocumentID == requested.TargetDocumentID &&
		existing.Type == requested.Type && existing.ContentHash == requested.ContentHash &&
		existing.ExpectedRevision == requested.ExpectedRevision
}
