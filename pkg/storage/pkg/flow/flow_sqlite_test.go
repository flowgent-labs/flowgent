package flow

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

func TestSQLiteCustomQueriesHonorFlowgentSqlScope(t *testing.T) {
	db := storage.NewSQLiteConn(context.Background(), t.TempDir())
	t.Cleanup(func() { _ = db.Close() })
	repo := NewFlowSQLiteStore(db)
	for _, namespace := range []string{"allowed", "blocked"} {
		spec := &entities.FlowInfo{BaseEntity: entities.BaseEntity{ID: "demo", Namespace: namespace}}
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
