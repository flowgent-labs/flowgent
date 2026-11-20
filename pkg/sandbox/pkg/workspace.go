package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// WorkspaceStore uses the existing durable workspace/PVC. A snapshot is an
// immutable directory plus a content manifest, published by one atomic rename.
// Capture must run after the workspace's execution owner has stopped writers.
type WorkspaceStore struct {
	Root     string
	MaxBytes int64
}
type workspaceFile struct {
	Path      string `json:"path"`
	Mode      uint32 `json:"mode"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256,omitempty"`
	Link      string `json:"link,omitempty"`
	Directory bool   `json:"directory,omitempty"`
}
type workspaceManifest struct {
	Source     string          `json:"source"`
	BaseCommit string          `json:"base_commit,omitempty"`
	Files      []workspaceFile `json:"files"`
}

func (s WorkspaceStore) Capture(ctx context.Context, source string) (*entities.WorkspaceSnapshot, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("workspace root must be an owned directory")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return nil, err
	}
	if source == root || insidePath(source, root) {
		return nil, fmt.Errorf("snapshot store must be outside captured workspace")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(root, ".pending-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	manifest := workspaceManifest{Source: source, Files: []workspaceFile{}}
	commit, _ := exec.CommandContext(ctx, "git", "-C", source, "rev-parse", "HEAD").Output()
	manifest.BaseCommit = strings.TrimSpace(string(commit))
	limit := s.MaxBytes
	if limit == 0 {
		limit = 512 << 20
	}
	var total int64
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if !safeRelative(rel) {
			return fmt.Errorf("invalid workspace path")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := workspaceFile{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm()), Size: info.Size(), Directory: d.IsDir()}
		destination := filepath.Join(stage, "files", rel)
		switch {
		case d.IsDir():
			err = os.MkdirAll(destination, info.Mode().Perm())
			if err == nil {
				err = os.Chmod(destination, info.Mode().Perm())
			}
		case info.Mode()&os.ModeSymlink != 0:
			entry.Link, err = os.Readlink(path)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(rel), entry.Link))
			if filepath.IsAbs(entry.Link) || !safeRelative(resolved) {
				return fmt.Errorf("workspace symlink escapes snapshot: %s", rel)
			}
			err = os.MkdirAll(filepath.Dir(destination), 0700)
			if err == nil {
				err = os.Symlink(entry.Link, destination)
			}
		case info.Mode().IsRegular():
			total += info.Size()
			if total > limit {
				return fmt.Errorf("workspace snapshot exceeds %d bytes", limit)
			}
			entry.SHA256, err = s.captureFile(path, destination, info.Mode().Perm())
			if err == nil {
				after, e := os.Stat(path)
				if e != nil {
					return e
				}
				if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
					return fmt.Errorf("workspace changed during snapshot: %s", rel)
				}
			}
		default:
			return fmt.Errorf("unsupported workspace entry: %s", rel)
		}
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	destination := filepath.Join(root, digest)
	if err = writeWorkspaceFile(filepath.Join(stage, "manifest.json"), data, 0600); err != nil {
		return nil, err
	}
	if err = filepath.WalkDir(stage, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return syncWorkspaceDir(path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err = os.Rename(stage, destination); err != nil {
		if _, exists := os.Stat(destination); exists != nil {
			return nil, err
		}
	}
	if err = syncWorkspaceDir(root); err != nil {
		return nil, err
	}
	snapshot := &entities.WorkspaceSnapshot{ID: digest, Ref: destination, SHA256: digest, BaseCommit: manifest.BaseCommit, CreatedAt: time.Now().UTC()}
	if err = s.Verify(ctx, snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}
func (s WorkspaceStore) manifest(snapshot *entities.WorkspaceSnapshot) (*workspaceManifest, error) {
	if snapshot == nil || len(snapshot.SHA256) != 64 || snapshot.ID != snapshot.SHA256 {
		return nil, fmt.Errorf("invalid workspace snapshot identity")
	}
	if _, err := hex.DecodeString(snapshot.SHA256); err != nil {
		return nil, fmt.Errorf("invalid snapshot digest")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return nil, err
	}
	expected := filepath.Join(root, snapshot.ID)
	if filepath.Clean(snapshot.Ref) != expected {
		return nil, fmt.Errorf("snapshot reference outside workspace store")
	}
	data, err := os.ReadFile(filepath.Join(expected, "manifest.json"))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != snapshot.SHA256 {
		return nil, fmt.Errorf("workspace manifest checksum mismatch")
	}
	var m workspaceManifest
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.BaseCommit != snapshot.BaseCommit {
		return nil, fmt.Errorf("workspace base commit mismatch")
	}
	return &m, nil
}
func (s WorkspaceStore) Verify(ctx context.Context, snapshot *entities.WorkspaceSnapshot) error {
	m, err := s.manifest(snapshot)
	if err != nil {
		return err
	}
	for _, file := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !safeRelative(file.Path) {
			return fmt.Errorf("invalid snapshot entry")
		}
		path := filepath.Join(snapshot.Ref, "files", file.Path)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != fs.FileMode(file.Mode) {
			return fmt.Errorf("snapshot mode mismatch: %s", file.Path)
		}
		if file.Directory {
			if !info.IsDir() {
				return fmt.Errorf("snapshot directory mismatch")
			}
			continue
		}
		if file.Link != "" {
			target, err := os.Readlink(path)
			if err != nil || target != file.Link {
				return fmt.Errorf("snapshot link mismatch")
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Size() != file.Size {
			return fmt.Errorf("snapshot size mismatch: %s", file.Path)
		}
		digest, err := hashWorkspaceFile(path)
		if err != nil {
			return err
		}
		if digest != file.SHA256 {
			return fmt.Errorf("snapshot file checksum mismatch: %s", file.Path)
		}
	}
	return nil
}

// Restore verifies before touching the destination. The old workspace is kept
// until the replacement is installed, so failed verification never rolls back
// a workspace to a partially written directory.
func (s WorkspaceStore) Restore(ctx context.Context, snapshot *entities.WorkspaceSnapshot, destination string) error {
	if err := s.Verify(ctx, snapshot); err != nil {
		return err
	}
	m, err := s.manifest(snapshot)
	if err != nil {
		return err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if destination != m.Source {
		return fmt.Errorf("workspace restore must target its owned source path")
	}
	parent := filepath.Dir(destination)
	stage, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, file := range m.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(stage, file.Path)
		if file.Directory {
			err = os.MkdirAll(target, fs.FileMode(file.Mode))
		} else if file.Link != "" {
			err = os.MkdirAll(filepath.Dir(target), 0700)
			if err == nil {
				err = os.Symlink(file.Link, target)
			}
		} else {
			_, err = copyWorkspaceFile(filepath.Join(snapshot.Ref, "files", file.Path), target, fs.FileMode(file.Mode))
		}
		if err != nil {
			return err
		}
	}
	if m.BaseCommit != "" {
		commit, err := exec.CommandContext(ctx, "git", "-C", stage, "rev-parse", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(commit)) != m.BaseCommit {
			return fmt.Errorf("restored Git base commit failed verification")
		}
	}
	backup := stage + ".old"
	hadOld := false
	if _, err := os.Lstat(destination); err == nil {
		if err = os.Rename(destination, backup); err != nil {
			return err
		}
		hadOld = true
	}
	if err = os.Rename(stage, destination); err != nil {
		if hadOld {
			_ = os.Rename(backup, destination)
		}
		return err
	}
	if err = syncWorkspaceDir(parent); err != nil {
		return err
	}
	if hadOld {
		return os.RemoveAll(backup)
	}
	return nil
}
func insidePath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func safeRelative(path string) bool {
	return path != "" && path != "." && !filepath.IsAbs(path) && path != ".." && !strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator))
}
func hashWorkspaceFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func copyWorkspaceFile(source, destination string, mode fs.FileMode) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", err
	}
	from, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer from.Close()
	to, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	defer to.Close()
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(to, hash), from); err != nil {
		return "", err
	}
	if err = to.Chmod(mode); err != nil {
		return "", err
	}
	if err = to.Sync(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func writeWorkspaceFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
func syncWorkspaceDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func lockWorkspace(ctx context.Context, root, source string) (func(), error) {
	sum := sha256.Sum256([]byte(source))
	directory := filepath.Join(root, ".workspace-locks")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, hex.EncodeToString(sum[:])+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			file.Close()
			return nil, err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Unchanged files share content-addressed objects; a checkpoint does not copy
// an entire Git checkout again. Metadata and integrity remain per snapshot.
func (s WorkspaceStore) captureFile(source, destination string, mode fs.FileMode) (string, error) {
	hash, err := hashWorkspaceFile(source)
	if err != nil {
		return "", err
	}
	objects := filepath.Join(s.Root, "objects")
	if err = os.MkdirAll(objects, 0700); err != nil {
		return "", err
	}
	object := filepath.Join(objects, hash+"-"+strconv.FormatUint(uint64(mode.Perm()), 8))
	if _, err = os.Stat(object); os.IsNotExist(err) {
		temporary, err := os.CreateTemp(objects, ".object-")
		if err != nil {
			return "", err
		}
		name := temporary.Name()
		temporary.Close()
		os.Remove(name)
		defer os.Remove(name)
		copied, err := copyWorkspaceFile(source, name, mode)
		if err != nil {
			return "", err
		}
		if copied != hash {
			return "", fmt.Errorf("workspace changed during file snapshot")
		}
		if err = os.Link(name, object); err != nil && !os.IsExist(err) {
			return "", err
		}
		if err = syncWorkspaceDir(objects); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return "", err
	}
	if err = os.Link(object, destination); err != nil {
		return "", err
	}
	return hash, nil
}
