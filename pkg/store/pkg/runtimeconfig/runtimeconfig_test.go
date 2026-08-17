package runtimeconfig

import (
	"context"
	"strings"
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/secretbox"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

type testStore struct{ db any }

func (s *testStore) DB() any      { return s.db }
func (s *testStore) Close() error { return nil }

func TestSQLiteRepositoryRoundTripAndUpsert(t *testing.T) {
	db := storepkg.NewSQLiteConn(context.Background(), t.TempDir())
	defer db.Close()
	repo, err := NewRepository(&testStore{db: db})
	if err != nil {
		t.Fatal(err)
	}
	var maintainerPermissions string
	if err := db.QueryRow(`SELECT permissions FROM iam_role WHERE id='builtin-maintainer'`).Scan(&maintainerPermissions); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(maintainerPermissions, `"flow.*"`) || !strings.Contains(maintainerPermissions, `"flow.config.manage"`) {
		t.Fatalf("maintainer runtime permissions are not least-privilege: %s", maintainerPermissions)
	}
	item := &entities.RuntimeConfiguration{
		BaseEntity: entities.BaseEntity{ID: "cfg-1", Namespace: "team-a"},
		ScopeType:  entities.RuntimeConfigScopeFlow, ScopeID: "fixer",
		Environment: map[string]string{"REGION": "cn"}, ConfiguredSecretKeys: []string{"TOKEN"},
		SealedSecrets: &secretbox.Envelope{Version: 1, Algorithm: "AES-256-GCM", KeyID: "v1", Nonce: "n", Ciphertext: "c"},
	}
	item.MarkCreated("tester")
	if err := repo.Upsert(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(context.Background(), "team-a", entities.RuntimeConfigScopeFlow, "fixer")
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment["REGION"] != "cn" || len(got.ConfiguredSecretKeys) != 1 || got.SealedSecrets == nil || got.SealedSecrets.Ciphertext != "c" {
		t.Fatalf("round trip = %+v", got)
	}
	item.Environment["REGION"] = "us"
	item.MarkUpdated("tester-2")
	if err := repo.Upsert(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(context.Background(), "team-a", entities.RuntimeConfigScopeFlow, "fixer")
	if err != nil || got.Environment["REGION"] != "us" || got.ID != "cfg-1" {
		t.Fatalf("upsert = %+v err=%v", got, err)
	}
}
