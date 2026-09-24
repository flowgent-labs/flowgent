package knowledge_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/flow"
	"github.com/flowgent-labs/flowgent/storage/pkg/flowrun"
	"github.com/flowgent-labs/flowgent/storage/pkg/knowledge"
	"github.com/google/uuid"
)

func TestSQLiteCandidateApprovalPublicationAndVectorIsolation(t *testing.T) {
	ctx := context.Background()
	db := storage.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()

	flowStore := flow.NewFlowSQLiteStore(db)
	flowSpec := &entities.FlowInfo{
		BaseEntity:       entities.BaseEntity{ID: "summarizer", Namespace: "team-summary"},
		Kind:             "flow",
		SummarizeEnabled: true,
	}
	if err := flowStore.SaveSpec(ctx, flowSpec, "tester", "enable reviewed summaries"); err != nil {
		t.Fatal(err)
	}
	otherFlow := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: "other", Namespace: "team-summary"}, Kind: "flow",
	}
	if err := flowStore.SaveSpec(ctx, otherFlow, "tester", "scope fixture"); err != nil {
		t.Fatal(err)
	}
	run := &entities.FlowRunInfo{
		BaseEntity: entities.BaseEntity{Namespace: "team-summary"}, AgentFlowID: "summarizer",
		Version: 1, Status: entities.RunCompleted,
	}
	if err := flowrun.NewFlowRunSQLiteStore(db).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if !run.SummarizeEnabled {
		t.Fatal("run did not persist the effective summarize flag")
	}

	store := knowledge.NewKnowledgeSQLiteStore(db)
	candidate := &entities.KnowledgeCandidate{
		BaseEntity:  entities.BaseEntity{Namespace: "team-summary", CreatedBy: "service:jobmaster"},
		SourceRunID: run.ID, TargetScope: "flow", TargetFlowName: "summarizer",
		Type: "knowledge", Content: "alpha reviewed operational fact",
		Provenance: map[string]any{"origin": "built_in_summarizer"}, ExpectedRevision: 0,
		IdempotencyKey: "summary:test:alpha",
	}
	approval, err := store.CreateCandidate(ctx, candidate)
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if approval.Type != "knowledge_publish" || approval.RequestHash == "" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	if candidate.TargetFlowID == "" || candidate.TargetFlowID == "summarizer" {
		t.Fatalf("target flow was not resolved to stable identity: %+v", candidate)
	}

	immutable := *candidate
	immutable.Content = "different body"
	immutable.ID = ""
	if _, err := store.CreateCandidate(ctx, &immutable); !errors.Is(err, knowledge.ErrCandidateImmutable) {
		t.Fatalf("same idempotency key with different body: %v", err)
	}

	published, err := store.ResolveCandidateApproval(ctx, approval.ID, true, "principal:reviewer", nil)
	if err != nil {
		t.Fatalf("ResolveCandidateApproval: %v", err)
	}
	documentID, _ := published.Metadata["published_id"].(string)
	if documentID == "" {
		t.Fatalf("published document ID missing: %+v", published)
	}
	if _, err = store.ResolveCandidateApproval(ctx, approval.ID, true, "principal:reviewer", nil); err != nil {
		t.Fatalf("duplicate approval callback must be idempotent: %v", err)
	}
	entry, err := store.Get(ctx, "team-summary", documentID)
	if err != nil || entry.Revision != 1 || entry.Content != candidate.Content {
		t.Fatalf("published knowledge = %+v, err=%v", entry, err)
	}
	results, err := store.Search(ctx, "team-summary", entities.KnowledgeSearchRequest{
		Query: "alpha", Scope: "flow", FlowName: "summarizer", TopK: 5,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("flow FTS results=%d err=%v", len(results), err)
	}
	otherResults, err := store.Search(ctx, "team-summary", entities.KnowledgeSearchRequest{
		Query: "alpha", Scope: "flow", FlowName: "other", TopK: 5,
	})
	if err != nil || len(otherResults) != 0 {
		t.Fatalf("cross-flow knowledge leaked: results=%d err=%v", len(otherResults), err)
	}

	profile := &entities.EmbeddingProfile{BaseEntity: entities.BaseEntity{Namespace: "team-summary"},
		ProfileKey: "tiny-v1", ProviderType: "openai", Model: "fixture", ModelRevision: "1",
		Dimensions: 3, DistanceMetric: "cosine"}
	if err = store.CreateEmbeddingProfile(ctx, profile); err != nil {
		t.Fatalf("CreateEmbeddingProfile: %v", err)
	}
	if err = store.PutEmbedding(ctx, &entities.KnowledgeEmbedding{
		BaseEntity: entities.BaseEntity{Namespace: "team-summary"}, ContentID: entry.ContentID,
		ProfileKey: "tiny-v1", Embedding: []float32{1, 0, 0},
	}); err != nil {
		t.Fatalf("PutEmbedding: %v", err)
	}
	vectorResults, err := store.Search(ctx, "team-summary", entities.KnowledgeSearchRequest{
		Embedding: []float32{0.9, 0.1, 0}, ProfileKey: "tiny-v1", Scope: "flow",
		FlowName: "summarizer", TopK: 5,
	})
	if err != nil || len(vectorResults) != 1 {
		t.Fatalf("sqlite-vec results=%d err=%v", len(vectorResults), err)
	}
	if err = store.PutEmbedding(ctx, &entities.KnowledgeEmbedding{
		BaseEntity: entities.BaseEntity{Namespace: "team-summary"}, ContentID: entry.ContentID,
		ProfileKey: "tiny-v1", Embedding: []float32{1, 0},
	}); err == nil {
		t.Fatal("dimension mismatch unexpectedly accepted")
	}

	instruction := &entities.KnowledgeCandidate{
		BaseEntity: entities.BaseEntity{Namespace: "team-summary"}, SourceRunID: run.ID,
		TargetScope: "flow", TargetFlowName: "summarizer", Type: "instruction",
		Content: "Always cite the verified source.", Provenance: map[string]any{"origin": "reviewer"},
		ExpectedRevision: 0, IdempotencyKey: "summary:test:instruction",
	}
	instructionApproval, err := store.CreateCandidate(ctx, instruction)
	if err != nil {
		t.Fatalf("CreateCandidate instruction: %v", err)
	}
	if _, err = store.ResolveCandidateApproval(ctx, instructionApproval.ID, true, "principal:reviewer", nil); err != nil {
		t.Fatalf("publish instruction: %v", err)
	}
	var instructionRevision int64
	var instructionContent string
	if err = db.QueryRowContext(ctx, `SELECT i.revision,i.content FROM orh_flow f
		JOIN llm_instruction i ON i.id=f.instruction_id WHERE f.id=?`, run.FlowID).
		Scan(&instructionRevision, &instructionContent); err != nil {
		t.Fatal(err)
	}
	if instructionRevision != 1 || instructionContent != instruction.Content {
		t.Fatalf("instruction revision=%d content=%q", instructionRevision, instructionContent)
	}

	firstUpdate := newCandidateForDocument(run, documentID, "alpha revision two", "summary:test:update-a")
	secondUpdate := newCandidateForDocument(run, documentID, "alpha competing revision", "summary:test:update-b")
	firstApproval, err := store.CreateCandidate(ctx, firstUpdate)
	if err != nil {
		t.Fatal(err)
	}
	secondApproval, err := store.CreateCandidate(ctx, secondUpdate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveCandidateApproval(ctx, firstApproval.ID, true, "principal:reviewer", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveCandidateApproval(ctx, secondApproval.ID, true, "principal:reviewer", nil); !errors.Is(err, knowledge.ErrPublicationStale) {
		t.Fatalf("concurrent baseline was not rejected: %v", err)
	}
	var staleApprovalStatus, staleCandidateStatus string
	if err = db.QueryRowContext(ctx, `SELECT a.status,c.status FROM orh_approval a
		JOIN knw_candidate c ON c.approval_id=a.id WHERE a.id=?`, secondApproval.ID).
		Scan(&staleApprovalStatus, &staleCandidateStatus); err != nil {
		t.Fatal(err)
	}
	if staleApprovalStatus != "approved" || staleCandidateStatus != "failed" {
		t.Fatalf("stale publication states approval=%s candidate=%s", staleApprovalStatus, staleCandidateStatus)
	}
}

func newCandidateForDocument(run *entities.FlowRunInfo, documentID, content, key string) *entities.KnowledgeCandidate {
	return &entities.KnowledgeCandidate{
		BaseEntity: entities.BaseEntity{Namespace: run.Namespace}, SourceRunID: run.ID,
		TargetScope: "flow", TargetFlowID: run.FlowID, TargetDocumentID: documentID,
		Type: "knowledge", Content: content, Provenance: map[string]any{"origin": "reviewer"},
		ExpectedRevision: 1, IdempotencyKey: key,
	}
}

func TestPostgresCandidateApprovalPublicationAndNativeVector(t *testing.T) {
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool := storage.NewPostgresPool(ctx, dsn, "public")
	defer pool.Close()
	suffix := uuid.NewString()[:8]
	namespace, flowName := "pg-summary-"+suffix, "summarizer-"+suffix
	flowSpec := &entities.FlowInfo{
		BaseEntity: entities.BaseEntity{ID: flowName, Namespace: namespace}, Kind: "flow", SummarizeEnabled: true,
	}
	if err := flow.NewFlowPostgresStore(pool).SaveSpec(ctx, flowSpec, "tester", "pgvector fixture"); err != nil {
		t.Fatal(err)
	}
	run := &entities.FlowRunInfo{BaseEntity: entities.BaseEntity{Namespace: namespace},
		AgentFlowID: flowName, Version: 1, Status: entities.RunCompleted}
	if err := flowrun.NewFlowRunPostgresStore(pool).Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	store := knowledge.NewKnowledgePostgresStore(pool)
	candidate := &entities.KnowledgeCandidate{
		BaseEntity: entities.BaseEntity{Namespace: namespace}, SourceRunID: run.ID,
		TargetScope: "flow", TargetFlowName: flowName, Type: "knowledge",
		Content: "native pgvector verified fact", Provenance: map[string]any{"origin": "built_in_summarizer"},
		ExpectedRevision: 0, IdempotencyKey: "summary:" + suffix,
	}
	approval, err := store.CreateCandidate(ctx, candidate)
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	published, err := store.ResolveCandidateApproval(ctx, approval.ID, true, "principal:reviewer", nil)
	if err != nil {
		t.Fatalf("ResolveCandidateApproval: %v", err)
	}
	documentID, _ := published.Metadata["published_id"].(string)
	entry, err := store.Get(ctx, namespace, documentID)
	if err != nil {
		t.Fatal(err)
	}
	profileKey := "pg-native-" + suffix
	if err = store.CreateEmbeddingProfile(ctx, &entities.EmbeddingProfile{
		BaseEntity: entities.BaseEntity{Namespace: namespace}, ProfileKey: profileKey,
		ProviderType: "openai", Model: "fixture", ModelRevision: "1", Dimensions: 3,
		DistanceMetric: "cosine",
	}); err != nil {
		t.Fatalf("CreateEmbeddingProfile: %v", err)
	}
	if err = store.PutEmbedding(ctx, &entities.KnowledgeEmbedding{
		BaseEntity: entities.BaseEntity{Namespace: namespace}, ContentID: entry.ContentID,
		ProfileKey: profileKey, Embedding: []float32{1, 0, 0},
	}); err != nil {
		t.Fatalf("native vector insert: %v", err)
	}
	results, err := store.Search(ctx, namespace, entities.KnowledgeSearchRequest{
		Embedding: []float32{0.8, 0.2, 0}, ProfileKey: profileKey, Scope: "flow",
		FlowName: flowName, TopK: 5,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("native vector search results=%d err=%v", len(results), err)
	}
	var vectorType, extensionVersion string
	err = pool.QueryRow(ctx, `SELECT pg_typeof(embedding)::text FROM knw_embedding
		WHERE content_id=$1 AND profile_key=$2`, entry.ContentID, profileKey).Scan(&vectorType)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname='vector'`).Scan(&extensionVersion); err != nil {
		t.Fatal(err)
	}
	if vectorType != "vector" || extensionVersion == "" {
		t.Fatalf("vector type=%q extension=%q", vectorType, extensionVersion)
	}
}
