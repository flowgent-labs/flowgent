package agentgraph

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

func TestSQLiteCustomQueriesHonorFlowgentSqlScope(t *testing.T) {
	db := storage.NewSQLiteConn(context.Background(), t.TempDir())
	t.Cleanup(func() { _ = db.Close() })
	repo := NewAgentGraphSQLiteStore(db)
	for _, namespace := range []string{"allowed", "blocked"} {
		spec := &entities.AgentGraph{Name: "demo", BaseEntity: entities.BaseEntity{ID: "demo", Namespace: namespace}}
		if err := repo.CreateSpec(context.Background(), spec, "test", "scope fixture"); err != nil {
			t.Fatalf("CreateSpec(%s): %v", namespace, err)
		}
	}

	ctx := storage.WithFlowgentSqlScope(context.Background(), storage.NamespaceFlowgentSqlScope("allowed"))
	if _, err := repo.Get(ctx, "allowed", "demo"); err != nil {
		t.Fatalf("Get allowed: %v", err)
	}
	if _, err := repo.Get(ctx, "blocked", "demo"); err == nil {
		t.Fatal("Get returned a row outside the AuthGuard SQL scope")
	}
	page, err := repo.Select(ctx, "blocked", entities.PageRequest{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("Select blocked: %v", err)
	}
	if page.TotalCount != 0 || len(page.Items) != 0 {
		t.Fatalf("Select blocked = total %d, items %d", page.TotalCount, len(page.Items))
	}
}

func TestSQLiteSaveSpecCreatesImmutableVersionsAndListsTheLatest(t *testing.T) {
	db := storage.NewSQLiteConn(context.Background(), t.TempDir())
	t.Cleanup(func() { _ = db.Close() })
	repo := NewAgentGraphSQLiteStore(db)
	ctx := context.Background()

	spec := &entities.AgentGraph{Name: "versioned-graph",
		BaseEntity: entities.BaseEntity{ID: "versioned-graph", Namespace: "default", Description: "v1"},
		Kind:       "graph",
	}
	if err := repo.CreateSpec(ctx, spec, "test", "create"); err != nil {
		t.Fatalf("CreateSpec: %v", err)
	}
	spec.Description = "v2"
	if err := repo.SaveSpec(ctx, spec, "test", "update"); err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if spec.Revision != 2 {
		t.Fatalf("updated version = %d, want 2", spec.Revision)
	}

	v1, err := repo.GetVersion(ctx, "default", "versioned-graph", 1)
	if err != nil {
		t.Fatalf("GetVersion(1): %v", err)
	}
	v2, err := repo.GetVersion(ctx, "default", "versioned-graph", 2)
	if err != nil {
		t.Fatalf("GetVersion(2): %v", err)
	}
	if string(v1.Definition) == string(v2.Definition) {
		t.Fatal("version 1 was overwritten instead of retaining its definition")
	}
	latest, err := repo.GetSpec(ctx, "default", "versioned-graph")
	if err != nil {
		t.Fatalf("GetSpec: %v", err)
	}
	if latest.Revision != 2 || latest.Description != "v2" {
		t.Fatalf("latest = version %d description %q, want version 2 description v2", latest.Revision, latest.Description)
	}
	page, err := repo.Select(ctx, "default", entities.PageRequest{Page: 1, Size: 20})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if page.TotalCount != 1 || len(page.Items) != 1 || page.Items[0].Revision != 2 {
		t.Fatalf("Select = total %d, items %#v; want one latest version", page.TotalCount, page.Items)
	}
}
