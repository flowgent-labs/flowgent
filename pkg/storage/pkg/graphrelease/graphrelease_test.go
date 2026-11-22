package graphrelease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	graph "github.com/flowgent-labs/flowgent/storage/pkg/agentgraph"
)

func TestSQLiteReleaseGrantAndAtomicInstall(t *testing.T) {
	ctx := context.Background()
	db := storage.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()
	repo := newSQLiteRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	producerAgentGraph := &entities.AgentGraph{Name: "shared-graph",
		BaseEntity: entities.BaseEntity{ID: "shared-graph", Namespace: "producer", Status: "ACTIVE"},
		Kind:       "graph", Nodes: []entities.GraphNode{{ID: "agent", Agent: "security-agent"}},
		RuntimeMode: entities.RuntimeModeApplication,
	}
	if err := graph.NewAgentGraphSQLiteStore(db).CreateSpec(ctx, producerAgentGraph, "producer-owner", "release fixture"); err != nil {
		t.Fatal(err)
	}
	release := &entities.AgentGraphRelease{
		BaseEntity:   entities.BaseEntity{ID: "release-1", Namespace: "producer"},
		AgentGraphID: "shared-graph", AgentGraphRevision: 1, ReleaseVersion: "1.0.0",
		Definition: entities.AgentGraph{Name: "shared-graph",
			BaseEntity:  entities.BaseEntity{ID: "shared-graph", Status: "ACTIVE"},
			Nodes:       []entities.GraphNode{{ID: "agent", Agent: "security-agent"}},
			RuntimeMode: entities.RuntimeModeApplication,
		},
		Checksum: strings.Repeat("a", 64), Visibility: "PRIVATE", PublishedAt: now,
	}
	release.MarkCreated("producer-owner")
	if err := repo.SaveRelease(ctx, release); err != nil {
		t.Fatalf("SaveRelease: %v", err)
	}
	if err := repo.SaveRelease(ctx, release); err == nil {
		t.Fatal("immutable release was overwritten")
	}
	if allowed, err := repo.CanAccessRelease(ctx, release.ID, "default"); err != nil || allowed {
		t.Fatalf("private release access before grant = %v, err=%v", allowed, err)
	}
	grant := &entities.AgentGraphReleaseGrant{
		BaseEntity:          entities.BaseEntity{ID: "grant-1", Namespace: "producer"},
		ReleaseID:           release.ID,
		ConsumerNamespaceID: "default",
	}
	grant.MarkCreated("producer-owner")
	if err := repo.SaveGrant(ctx, grant); err != nil {
		t.Fatalf("SaveGrant: %v", err)
	}
	if allowed, err := repo.CanAccessRelease(ctx, release.ID, "default"); err != nil || !allowed {
		t.Fatalf("private release access after grant = %v, err=%v", allowed, err)
	}

	definition := release.Definition
	definition.ID = "installed-graph"
	definition.Namespace = "default"
	installation := &entities.AgentGraphInstallation{
		BaseEntity:            entities.BaseEntity{ID: "installation-1", Namespace: "default"},
		ReleaseID:             release.ID,
		ReleaseVersion:        release.ReleaseVersion,
		ProducerNamespaceID:   release.Namespace,
		InstalledAgentGraphID: definition.ID,
		ReleaseChecksum:       release.Checksum,
		AppliedChecksum:       strings.Repeat("b", 64),
		ResourceBindings:      map[string]string{"agent:security-agent": "local-agent"},
		InstalledAt:           now,
	}
	installation.MarkCreated("consumer-owner")
	if err := repo.Install(ctx, &definition, installation, "consumer-owner"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installed, err := graph.NewAgentGraphSQLiteStore(db).GetSpec(ctx, "default", definition.ID)
	if err != nil || installed.Namespace != "default" {
		t.Fatalf("installed graph = %+v, err=%v", installed, err)
	}
	if err := repo.Install(ctx, &definition, &entities.AgentGraphInstallation{
		BaseEntity: entities.BaseEntity{ID: "installation-2", Namespace: "default"},
	}, "consumer-owner"); err == nil {
		t.Fatal("duplicate installed graph was accepted")
	}
	installations, err := repo.ListInstallations(ctx, "default")
	if err != nil || len(installations) != 1 || installations[0].ReleaseID != release.ID {
		t.Fatalf("installations = %+v, err=%v", installations, err)
	}
	consumerScope := storage.WithFlowgentSqlScope(ctx, storage.NamespaceFlowgentSqlScope("default"))
	installations, err = repo.ListInstallations(consumerScope, "default")
	if err != nil || len(installations) != 1 {
		t.Fatalf("scoped installations = %+v, err=%v", installations, err)
	}
	producerScope := storage.WithFlowgentSqlScope(ctx, storage.NamespaceFlowgentSqlScope("producer"))
	installations, err = repo.ListInstallations(producerScope, "default")
	if err != nil || len(installations) != 0 {
		t.Fatalf("cross-scope installations = %+v, err=%v", installations, err)
	}
	if err := repo.RevokeRelease(ctx, "producer", release.ID, "producer-owner"); err != nil {
		t.Fatalf("RevokeRelease: %v", err)
	}
	if allowed, err := repo.CanAccessRelease(ctx, release.ID, "default"); err != nil || allowed {
		t.Fatalf("revoked release access = %v, err=%v", allowed, err)
	}
}
