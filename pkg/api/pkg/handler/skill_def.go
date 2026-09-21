package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
		if err := h.attachFiles(namespace, item); err != nil {
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
	if err := h.attachFiles(namespace, item); err != nil {
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
	item.Version = 1
	item.Status = "ACTIVE"
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
	item.Version = existing.Version + 1
	item.Status = existing.Status
	item.CreatedAt = existing.CreatedAt
	item.CreatedBy = existing.CreatedBy
	item.UpdatedAt = time.Now()
	item.UpdatedBy = existing.UpdatedBy
	if err := h.renameWorkspace(namespace, name, item.Name); err != nil {
		http.Error(w, "rename skill workspace", http.StatusInternalServerError)
		return
	}
	if err := h.store.Save(r.Context(), &item); err != nil {
		if rollbackErr := h.renameWorkspace(namespace, item.Name, name); rollbackErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.attachFiles(namespace, &item); err != nil {
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
	path, err := h.skillPath(namespace, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.RemoveAll(path); err != nil {
		http.Error(w, "remove skill workspace", http.StatusInternalServerError)
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
	if _, err := h.store.Get(r.Context(), namespace, name); err != nil {
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
	directory, err := h.fileDirectory(namespace, name, kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(directory, 0750); err != nil {
		http.Error(w, "create skill workspace", http.StatusInternalServerError)
		return
	}
	temp, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		http.Error(w, "create upload", http.StatusInternalServerError)
		return
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	size, copyErr := io.Copy(temp, io.LimitReader(file, maxSkillUploadBytes+1))
	if closeErr := temp.Close(); copyErr != nil || closeErr != nil {
		http.Error(w, "write upload", http.StatusInternalServerError)
		return
	}
	if size > maxSkillUploadBytes {
		http.Error(w, "file exceeds 10 MiB limit", http.StatusRequestEntityTooLarge)
		return
	}
	if kind == "scripts" {
		err = os.Chmod(tempName, 0750)
	} else {
		err = os.Chmod(tempName, 0640)
	}
	if err != nil {
		http.Error(w, "secure upload", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tempName, filepath.Join(directory, filename)); err != nil {
		http.Error(w, "finalize upload", http.StatusInternalServerError)
		return
	}
	item, err := h.store.Get(r.Context(), namespace, name)
	if err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	if err := h.attachFiles(namespace, item); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, item, http.StatusOK)
}

func (h *SkillHandler) attachFiles(namespace string, item *entities.SkillInfo) error {
	assets, err := h.listFiles(namespace, item.Name, "assets")
	if err != nil {
		return err
	}
	scripts, err := h.listFiles(namespace, item.Name, "scripts")
	if err != nil {
		return err
	}
	item.Assets, item.Scripts = assets, scripts
	return nil
}

func (h *SkillHandler) listFiles(namespace, name, kind string) ([]entities.SkillFile, error) {
	directory, err := h.fileDirectory(namespace, name, kind)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []entities.SkillFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]entities.SkillFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, entities.SkillFile{
			Name:        entry.Name(),
			ContentType: contentType(entry.Name()),
			Size:        info.Size(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func (h *SkillHandler) fileDirectory(namespace, name, kind string) (string, error) {
	if kind != "assets" && kind != "scripts" {
		return "", fmt.Errorf("invalid file kind")
	}
	base, err := h.skillPath(namespace, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, kind), nil
}

func (h *SkillHandler) skillPath(namespace, name string) (string, error) {
	if !resourceNamePattern.MatchString(namespace) || !resourceNamePattern.MatchString(name) {
		return "", fmt.Errorf("name may contain only letters, digits, hyphens, and underscores")
	}
	path := filepath.Join(h.workspace, namespace, name)
	root := h.workspace + string(os.PathSeparator)
	if !strings.HasPrefix(path+string(os.PathSeparator), root) {
		return "", fmt.Errorf("invalid skill workspace path")
	}
	return path, nil
}

func (h *SkillHandler) renameWorkspace(namespace, from, to string) error {
	if from == to {
		return nil
	}
	oldPath, err := h.skillPath(namespace, from)
	if err != nil {
		return err
	}
	newPath, err := h.skillPath(namespace, to)
	if err != nil {
		return err
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target skill workspace already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldPath, newPath)
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
