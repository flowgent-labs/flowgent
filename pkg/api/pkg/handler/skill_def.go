package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storage "github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/flowgent-labs/flowgent/storage/pkg/skill"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxSkillUploadBytes = 10 << 20

var (
	skillAssetExtensions = map[string]struct{}{
		".csv": {}, ".gif": {}, ".jpeg": {}, ".jpg": {}, ".json": {}, ".md": {},
		".pdf": {}, ".png": {}, ".txt": {}, ".webp": {}, ".xls": {}, ".xlsx": {},
		".yaml": {}, ".yml": {},
	}
	skillScriptExtensions = map[string]struct{}{
		".js": {}, ".py": {}, ".rb": {}, ".sh": {}, ".ts": {},
	}
)

// SkillHandler manages reusable prompt/tool definitions and their local
// workspace inputs. Executable kind=skill Flow definitions intentionally stay
// on the existing FlowDefHandler routes.
type SkillHandler struct {
	store     skill.ISkillStore
	workspace string
}

// NewSkillHandler creates a namespaced SkillHandler. FLOWGENT_SKILL_WORKSPACE
// allows operators to mount durable shared storage; the temp-directory default
// is safe for local development and test deployments.
func NewSkillHandler(s storage.IStorage) *SkillHandler {
	var skillStore skill.ISkillStore
	switch db := s.DB().(type) {
	case *pgxpool.Pool:
		skillStore = skill.NewSkillPostgresStore(db)
	case *sql.DB:
		skillStore = skill.NewSkillSQLiteStore(db)
	}
	workspace := strings.TrimSpace(os.Getenv("FLOWGENT_SKILL_WORKSPACE"))
	if workspace == "" {
		workspace = filepath.Join(os.TempDir(), "flowgent", "skills")
	}
	if absolute, err := filepath.Abs(workspace); err == nil {
		workspace = absolute
	}
	return &SkillHandler{store: skillStore, workspace: filepath.Clean(workspace)}
}

func (h *SkillHandler) List(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	page, err := h.store.List(r.Context(), namespace, entities.PageRequest{Page: 1, Size: 1000})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]*entities.SkillInfo, 0, len(page.Items))
	for _, item := range page.Items {
		if err := h.attachFiles(r.Context(), namespace, item); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, items, http.StatusOK)
}

func (h *SkillHandler) Get(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	item, err := h.store.Get(r.Context(), namespace, name)
	if err != nil || item == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	if err := h.attachFiles(r.Context(), namespace, item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, item, http.StatusOK)
}

func (h *SkillHandler) Create(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	var item entities.SkillInfo
	if err := decodeStrictJSON(r, &item); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateSkill(&item, namespace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if existing, err := h.store.Get(r.Context(), namespace, item.Name); err == nil && existing != nil {
		http.Error(w, "skill already exists", http.StatusConflict)
		return
	}
	item.ID = uuid.NewString()
	item.Namespace = namespace
	item.Revision = 1
	item.Version = 1
	item.Status = "ACTIVE"
	item.CreatedBy = authenticatedUserID(r.Context())
	item.UpdatedBy = item.CreatedBy
	item.CreatedAt = time.Now()
	item.UpdatedAt = item.CreatedAt
	if err := h.store.Save(r.Context(), &item); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, &item, http.StatusCreated)
}

func (h *SkillHandler) Update(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	existing, err := h.store.Get(r.Context(), namespace, name)
	if err != nil || existing == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	var item entities.SkillInfo
	if err := decodeStrictJSON(r, &item); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if item.Namespace != "" && item.Namespace != namespace {
		http.Error(w, "namespace mismatch", http.StatusBadRequest)
		return
	}
	if item.Name == "" {
		item.Name = name
	}
	item.Namespace = namespace
	if err := validateSkill(&item, namespace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if item.Name != name {
		if duplicate, err := h.store.Get(r.Context(), namespace, item.Name); err == nil && duplicate != nil {
			http.Error(w, "skill already exists", http.StatusConflict)
			return
		}
	}
	item.ID = existing.ID
	item.Revision = existing.Revision + 1
	item.Version = item.Revision
	item.Status = existing.Status
	item.CreatedAt = existing.CreatedAt
	item.CreatedBy = existing.CreatedBy
	item.UpdatedAt = time.Now()
	item.UpdatedBy = authenticatedUserID(r.Context())
	if err := h.store.Save(r.Context(), &item); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.attachFiles(r.Context(), namespace, &item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, &item, http.StatusOK)
}

func (h *SkillHandler) Delete(w http.ResponseWriter, r *http.Request) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	if _, err := h.store.Get(r.Context(), namespace, name); err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	if err := h.store.Delete(r.Context(), namespace, name); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UploadAsset stores a supported knowledge file in the Skill's assets/ folder.
func (h *SkillHandler) UploadAsset(w http.ResponseWriter, r *http.Request) {
	h.uploadFile(w, r, "assets", skillAssetExtensions)
}

// UploadScript stores a supported helper script in the Skill's scripts/ folder.
func (h *SkillHandler) UploadScript(w http.ResponseWriter, r *http.Request) {
	h.uploadFile(w, r, "scripts", skillScriptExtensions)
}

func (h *SkillHandler) uploadFile(w http.ResponseWriter, r *http.Request, kind string, allowed map[string]struct{}) {
	namespace, name := r.PathValue("namespace"), r.PathValue("name")
	existing, err := h.store.Get(r.Context(), namespace, name)
	if err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSkillUploadBytes)
	if err := r.ParseMultipartForm(maxSkillUploadBytes); err != nil {
		http.Error(w, "file exceeds 10 MiB limit", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	filename := filepath.Base(header.Filename)
	extension := strings.ToLower(filepath.Ext(filename))
	if filename == "." || filename == "" || filename != header.Filename {
		http.Error(w, "invalid file name", http.StatusBadRequest)
		return
	}
	if _, ok := allowed[extension]; !ok {
		http.Error(w, "unsupported file type", http.StatusBadRequest)
		return
	}
	stagingDirectory := filepath.Join(h.workspace, ".uploads")
	if err := os.MkdirAll(stagingDirectory, 0700); err != nil {
		http.Error(w, "create skill workspace", http.StatusInternalServerError)
		return
	}
	temp, err := os.CreateTemp(stagingDirectory, ".upload-*")
	if err != nil {
		http.Error(w, "create upload", http.StatusInternalServerError)
		return
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(file, maxSkillUploadBytes+1))
	if closeErr := temp.Close(); copyErr != nil || closeErr != nil {
		http.Error(w, "write upload", http.StatusInternalServerError)
		return
	}
	if size > maxSkillUploadBytes {
		http.Error(w, "file exceeds 10 MiB limit", http.StatusRequestEntityTooLarge)
		return
	}
	mode := os.FileMode(0640)
	if kind == "scripts" {
		mode = 0750
	}
	err = os.Chmod(tempName, mode)
	if err != nil {
		http.Error(w, "secure upload", http.StatusInternalServerError)
		return
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))
	blobPath, err := h.blobPath(namespace, existing.ID, kind, contentHash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0750); err != nil {
		http.Error(w, "create skill blob directory", http.StatusInternalServerError)
		return
	}
	if _, statErr := os.Stat(blobPath); os.IsNotExist(statErr) {
		if err := os.Rename(tempName, blobPath); err != nil {
			http.Error(w, "finalize upload", http.StatusInternalServerError)
			return
		}
	} else if statErr != nil {
		http.Error(w, "inspect skill blob", http.StatusInternalServerError)
		return
	}
	item, err := h.store.SaveFileRevision(r.Context(), namespace, name, entities.SkillFile{
		Kind:         strings.TrimSuffix(kind, "s"),
		RelativePath: filepath.ToSlash(filepath.Join(kind, filename)),
		MediaType:    contentType(filename),
		SizeBytes:    size,
		ContentHash:  contentHash,
		CreatedAt:    time.Now().UTC(),
		CreatedBy:    authenticatedUserID(r.Context()),
	}, authenticatedUserID(r.Context()))
	if err != nil {
		http.Error(w, "persist skill file revision", http.StatusInternalServerError)
		return
	}
	if err := h.attachFiles(r.Context(), namespace, item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, item, http.StatusOK)
}

func (h *SkillHandler) attachFiles(ctx context.Context, namespace string, item *entities.SkillInfo) error {
	files, err := h.store.ListFiles(ctx, namespace, item.Name)
	if err != nil {
		return err
	}
	assets := make([]entities.SkillFile, 0)
	scripts := make([]entities.SkillFile, 0)
	for _, file := range files {
		switch file.Kind {
		case "asset":
			assets = append(assets, file)
		case "script":
			scripts = append(scripts, file)
		}
	}
	item.Assets, item.Scripts = assets, scripts
	return nil
}

func (h *SkillHandler) skillPath(namespace, name string) (string, error) {
	if !resourceNamePattern.MatchString(namespace) || !resourceNamePattern.MatchString(name) {
		return "", fmt.Errorf("name may contain only letters, digits, hyphens, and underscores")
	}
	path := filepath.Join(h.workspace, namespace, name)
	relative, err := filepath.Rel(h.workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid skill workspace path")
	}
	return path, nil
}

func (h *SkillHandler) blobPath(namespace, skillID, kind, contentHash string) (string, error) {
	if kind != "assets" && kind != "scripts" {
		return "", fmt.Errorf("invalid file kind")
	}
	if len(contentHash) != sha256.Size*2 {
		return "", fmt.Errorf("invalid content hash")
	}
	base, err := h.skillPath(namespace, skillID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "blobs", kind, contentHash), nil
}

func validateSkill(item *entities.SkillInfo, namespace string) error {
	if item.Namespace != "" && item.Namespace != namespace {
		return fmt.Errorf("namespace mismatch")
	}
	if !resourceNamePattern.MatchString(item.Name) {
		return fmt.Errorf("name may contain only letters, digits, hyphens, and underscores")
	}
	if strings.TrimSpace(item.Instruction) == "" {
		return fmt.Errorf("instruction is required")
	}
	return nil
}

func contentType(filename string) string {
	if result := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); result != "" {
		return result
	}
	return "application/octet-stream"
}

func writeJSON(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
