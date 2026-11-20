package skill_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/skill"
)

func TestSQLiteSkillFilesFollowImmutableRevisions(t *testing.T) {
	ctx := context.Background()
	db := storage.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()
	store := skill.NewSkillSQLiteStore(db)
	item := &entities.SkillInfo{
		BaseEntity:  entities.BaseEntity{Namespace: "skill-team", CreatedBy: "principal:author"},
		Name:        "review-helper",
		Instruction: "Review the supplied change.",
	}
	if err := store.Save(ctx, item); err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if item.Revision != 1 {
		t.Fatalf("create revision=%d, want 1", item.Revision)
	}

	createdAt := time.Date(2026, 9, 24, 1, 2, 3, 0, time.UTC)
	item, err := store.SaveFileRevision(ctx, "skill-team", "review-helper", entities.SkillFile{
		Kind: "asset", RelativePath: "assets/policy.md", MediaType: "text/markdown",
		SizeBytes: 42, ContentHash: strings.Repeat("a", 64), CreatedAt: createdAt,
	}, "principal:uploader")
	if err != nil {
		t.Fatalf("upload asset: %v", err)
	}
	if item.Revision != 2 {
		t.Fatalf("upload revision=%d, want 2", item.Revision)
	}
	assertSkillFiles(t, store, ctx, "skill-team", "review-helper", 1, strings.Repeat("a", 64))

	item.Instruction = "Review the supplied change and cite the policy."
	item.UpdatedBy = "principal:editor"
	if err := store.Save(ctx, item); err != nil {
		t.Fatalf("edit skill: %v", err)
	}
	if item.Revision != 3 {
		t.Fatalf("edit revision=%d, want 3", item.Revision)
	}
	assertSkillFiles(t, store, ctx, "skill-team", "review-helper", 1, strings.Repeat("a", 64))

	item, err = store.SaveFileRevision(ctx, "skill-team", "review-helper", entities.SkillFile{
		Kind: "asset", RelativePath: "assets/policy.md", MediaType: "text/markdown",
		SizeBytes: 43, ContentHash: strings.Repeat("b", 64),
	}, "principal:uploader")
	if err != nil {
		t.Fatalf("replace asset: %v", err)
	}
	if item.Revision != 4 {
		t.Fatalf("replacement revision=%d, want 4", item.Revision)
	}
	assertSkillFiles(t, store, ctx, "skill-team", "review-helper", 1, strings.Repeat("b", 64))

	item, err = store.SaveFileRevision(ctx, "skill-team", "review-helper", entities.SkillFile{
		Kind: "script", RelativePath: "scripts/check.py", MediaType: "text/x-python",
		SizeBytes: 21, ContentHash: strings.Repeat("c", 64),
	}, "principal:uploader")
	if err != nil {
		t.Fatalf("upload script: %v", err)
	}
	if item.Revision != 5 {
		t.Fatalf("script revision=%d, want 5", item.Revision)
	}
	files, err := store.ListFiles(ctx, "skill-team", "review-helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Kind != "asset" || files[1].Kind != "script" {
		t.Fatalf("current files=%+v, want one asset and one script", files)
	}

	var revisions, currentRevisionFiles int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_skill_revision r JOIN llm_skill s ON s.id=r.skill_id WHERE s.namespace_id=? AND s.name=?`, "skill-team", "review-helper").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_skill_file f JOIN llm_skill s ON s.current_revision_id=f.skill_revision_id WHERE s.namespace_id=? AND s.name=?`, "skill-team", "review-helper").Scan(&currentRevisionFiles); err != nil {
		t.Fatal(err)
	}
	if revisions != 5 || currentRevisionFiles != 2 {
		t.Fatalf("revisions=%d current_files=%d, want 5/2", revisions, currentRevisionFiles)
	}
	if _, err = db.ExecContext(ctx, `UPDATE llm_skill_file SET size_bytes=size_bytes+1 WHERE id=?`, files[0].ID); err == nil {
		t.Fatal("append-only llm_skill_file unexpectedly allowed UPDATE")
	}
}

func assertSkillFiles(t *testing.T, store *skill.SkillSQLiteStore, ctx context.Context, namespace, name string, count int, hash string) {
	t.Helper()
	files, err := store.ListFiles(ctx, namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != count || files[0].ContentHash != hash {
		t.Fatalf("files=%+v, want count=%d hash=%s", files, count, hash)
	}
}
