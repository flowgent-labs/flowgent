package resourcepool

import (
	"context"
	"errors"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

func TestSQLiteResourcePoolRepositoryIsNamespaceScoped(t *testing.T) {
	ctx := context.Background()
	db := storepkg.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()
	repository := &sqliteRepository{db: db}

	create := func(namespace, name string) *entities.ResourcePoolInfo {
		t.Helper()
		item := &entities.ResourcePoolInfo{
			BaseEntity: entities.BaseEntity{Namespace: namespace},
			Name:       name, Replicas: 2, SlotsPerPod: 8,
			SandboxReplicas: 1, SandboxSlotsPerPod: 2,
		}
		if err := repository.Create(ctx, item); err != nil {
			t.Fatalf("Create(%s/%s): %v", namespace, name, err)
		}
		return item
	}
	teamA := create("team-a", "critical")
	create("team-b", "critical")

	duplicate := &entities.ResourcePoolInfo{BaseEntity: entities.BaseEntity{Namespace: "team-a"}, Name: "CRITICAL", Replicas: 1, SlotsPerPod: 1}
	if err := repository.Create(ctx, duplicate); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("case-insensitive duplicate error = %v, want %v", err, ErrAlreadyExists)
	}

	teamA.Replicas = 4
	teamA.NodeSelector = map[string]string{"workload": "critical"}
	if err := repository.Update(ctx, teamA); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, "team-a", "CRITICAL")
	if err != nil {
		t.Fatal(err)
	}
	if got.Replicas != 4 || got.NodeSelector["workload"] != "critical" {
		t.Fatalf("updated pool = %#v", got)
	}
	other, err := repository.Get(ctx, "team-b", "critical")
	if err != nil || other.Replicas != 2 {
		t.Fatalf("cross-namespace mutation: pool=%#v err=%v", other, err)
	}

	if err := repository.Delete(ctx, "team-a", "critical", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, "team-a", "critical"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get deleted error = %v, want %v", err, ErrNotFound)
	}
	if _, err := repository.Get(ctx, "team-b", "critical"); err != nil {
		t.Fatalf("team-b pool deleted with team-a: %v", err)
	}
}
