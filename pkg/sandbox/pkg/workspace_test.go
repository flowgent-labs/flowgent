package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorkspaceSnapshotRestoresOnlyVerifiedCompleteState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(source, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "code.go"), []byte("committed\n"), 0640); err != nil {
		t.Fatal(err)
	}
	store := WorkspaceStore{Root: filepath.Join(root, "snapshots")}
	snapshot, err := store.Capture(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "code.go"), []byte("half-written"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "partial.tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.Restore(ctx, snapshot, source); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source, "code.go"))
	if err != nil || string(data) != "committed\n" {
		t.Fatalf("restore %q: %v", data, err)
	}
	if _, err = os.Stat(filepath.Join(source, "partial.tmp")); !os.IsNotExist(err) {
		t.Fatal("partial modification survived restore")
	}
	if _, err = os.Stat(filepath.Join(source, "empty")); err != nil {
		t.Fatal("empty directory was lost", err)
	}
	// Corruption must be detected before replacing the live workspace.
	if err = os.WriteFile(filepath.Join(snapshot.Ref, "files", "code.go"), []byte("corrupt"), 0640); err != nil {
		t.Fatal(err)
	}
	if err = store.Restore(ctx, snapshot, source); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
	data, _ = os.ReadFile(filepath.Join(source, "code.go"))
	if string(data) != "committed\n" {
		t.Fatal("failed restore damaged live files")
	}
}
func TestWorkspaceSnapshotRejectsEscapeAndIncompleteCapture(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "workspace")
	os.Mkdir(source, 0700)
	store := WorkspaceStore{Root: filepath.Join(root, "snapshots"), MaxBytes: 2}
	os.WriteFile(filepath.Join(source, "big"), []byte("too large"), 0600)
	if _, err := store.Capture(context.Background(), source); err == nil {
		t.Fatal("size limit ignored")
	}
	entries, _ := os.ReadDir(store.Root)
	if len(entries) != 0 {
		t.Fatal("failed capture left a published snapshot")
	}
	os.Remove(filepath.Join(source, "big"))
	os.Symlink("../../outside", filepath.Join(source, "escape"))
	if _, err := store.Capture(context.Background(), source); err == nil {
		t.Fatal("escaping link captured")
	}
}
func TestWorkspaceSnapshotPreservesGitCommit(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "repository")
	os.Mkdir(source, 0700)
	command := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("git", append([]string{"-C", source}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", output, err)
		}
	}
	command("init", "--quiet")
	command("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "--allow-empty", "-m", "test: workspace fixture")
	os.WriteFile(filepath.Join(source, "untracked.txt"), []byte("before crash"), 0644)
	store := WorkspaceStore{Root: filepath.Join(root, "snapshots")}
	snapshot, err := store.Capture(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BaseCommit == "" {
		t.Fatal("Git base commit was not recorded")
	}
	os.WriteFile(filepath.Join(source, "untracked.txt"), []byte("partial edit"), 0644)
	if err = store.Restore(context.Background(), snapshot, source); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(source, "untracked.txt"))
	if string(data) != "before crash" {
		t.Fatal("untracked source not restored")
	}
}
