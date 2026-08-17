package flowrelease

import (
	"context"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/flowgent-labs/flowgent/store/pkg/flow"
)

func TestSQLiteReleaseGrantAndAtomicInstall(t *testing.T) {
	ctx := context.Background()
	db := storepkg.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()
	repo := newSQLiteRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	release := &entities.FlowRelease{
		BaseEntity: entities.BaseEntity{ID: "release-1", Namespace: "producer"},
		FlowID:     "shared-flow", FlowVersion: 1, ReleaseVersion: "1.0.0",
		Definition: entities.FlowInfo{
			BaseEntity: entities.BaseEntity{ID: "shared-flow", Status: "ACTIVE"},
			Nodes:      []entities.Node{{ID: "agent", Agent: "security-agent"}},
			ResourcePoolID: "default",
		},
		Checksum: "release-checksum", Visibility: "PRIVATE", PublishedAt: now,
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
	grant := &entities.FlowReleaseGrant{
		BaseEntity:        entities.BaseEntity{ID: "grant-1", Namespace: "producer"},
		ReleaseID:         release.ID,
		ConsumerNamespace: "default",
	}
	grant.MarkCreated("producer-owner")
	if err := repo.SaveGrant(ctx, grant); err != nil {
		t.Fatalf("SaveGrant: %v", err)
	}
	if allowed, err := repo.CanAccessRelease(ctx, release.ID, "default"); err != nil || !allowed {
		t.Fatalf("private release access after grant = %v, err=%v", allowed, err)
	}

	definition := release.Definition
	definition.ID = "installed-flow"
	definition.Namespace = "default"
	installation := &entities.FlowInstallation{
		BaseEntity:        entities.BaseEntity{ID: "installation-1", Namespace: "default"},
		ReleaseID:         release.ID,
		ReleaseVersion:    release.ReleaseVersion,
		ProducerNamespace: release.Namespace,
		InstalledFlowID:   definition.ID,
		ReleaseChecksum:   release.Checksum,
		AppliedChecksum:   "applied-checksum",
		ResourceBindings:  map[string]string{"agent:security-agent": "local-agent"},
		InstalledAt:       now,
	}
	installation.MarkCreated("consumer-owner")
	if err := repo.Install(ctx, &definition, installation, "consumer-owner"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installed, err := flow.NewFlowSQLiteStore(db).GetSpec(ctx, "default", definition.ID)
	if err != nil || installed.Namespace != "default" {
		t.Fatalf("installed flow = %+v, err=%v", installed, err)
	}
	if err := repo.Install(ctx, &definition, &entities.FlowInstallation{
		BaseEntity: entities.BaseEntity{ID: "installation-2", Namespace: "default"},
	}, "consumer-owner"); err == nil {
		t.Fatal("duplicate installed flow was accepted")
	}
	installations, err := repo.ListInstallations(ctx, "default")
	if err != nil || len(installations) != 1 || installations[0].ReleaseID != release.ID {
		t.Fatalf("installations = %+v, err=%v", installations, err)
	}
	if err := repo.RevokeRelease(ctx, "producer", release.ID, "producer-owner"); err != nil {
		t.Fatalf("RevokeRelease: %v", err)
	}
	if allowed, err := repo.CanAccessRelease(ctx, release.ID, "default"); err != nil || allowed {
		t.Fatalf("revoked release access = %v, err=%v", allowed, err)
	}
}
