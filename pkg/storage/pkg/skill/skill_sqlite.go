package skill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
)

type SkillSQLiteStore struct{ conn *sql.DB }

func NewSkillSQLiteStore(conn *sql.DB) *SkillSQLiteStore { return &SkillSQLiteStore{conn: conn} }
func (s *SkillSQLiteStore) Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[skillRecord]() + ` FROM (SELECT a.name,r.revision,r.instruction,r.model_config,r.tools,a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata FROM (SELECT * FROM llm_skill WHERE namespace_id=?1 AND name=?2 AND status<>'DELETED' AND (` + scopeWhere + `)) a JOIN llm_skill_revision r ON r.id=a.current_revision_id) current_skill`
	var record skillRecord
	if err := utils.ScanStruct(s.conn.QueryRowContext(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}
func (s *SkillSQLiteStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").SQLiteWhere()
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_skill WHERE namespace_id=?1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := `SELECT ` + utils.Columns[skillRecord]() + ` FROM (SELECT a.name,r.revision,r.instruction,r.model_config,r.tools,a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata FROM (SELECT * FROM llm_skill WHERE namespace_id=?1 AND status<>'DELETED' AND (` + scopeWhere + `)) a JOIN llm_skill_revision r ON r.id=a.current_revision_id) current_skill ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.SkillInfo, 0)
	for rows.Next() {
		var record skillRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			return nil, err
		}
		items = append(items, record.entity())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entities.NewPage(items, total, req), nil
}

func (s *SkillSQLiteStore) ListFiles(ctx context.Context, namespace, name string) ([]entities.SkillFile, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[skillFileRecord]() + ` FROM (SELECT f.id,f.kind,f.relative_path,f.media_type,f.size_bytes,f.content_hash,f.created_at,f.created_by,f.metadata FROM llm_skill_file f JOIN (SELECT * FROM llm_skill WHERE namespace_id=?1 AND name=?2 AND status<>'DELETED' AND (` + scopeWhere + `)) s ON s.current_revision_id=f.skill_revision_id) current_skill_files ORDER BY kind,relative_path`
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]entities.SkillFile, 0)
	for rows.Next() {
		var record skillFileRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			return nil, err
		}
		files = append(files, record.entity())
	}
	return files, rows.Err()
}

func (s *SkillSQLiteStore) Save(ctx context.Context, item *entities.SkillInfo) error {
	if item.Namespace == "" {
		item.Namespace = "default"
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	principal := item.UpdatedBy
	if principal == "" {
		principal = item.CreatedBy
	}
	if principal == "" {
		principal = "system:flowgent"
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO orh_namespace(id,name,description,created_by,updated_by) VALUES(?,?,?, ?,?)`, item.Namespace, item.Namespace, "Flowgent namespace", principal, principal); err != nil {
		return err
	}
	var id, currentRevisionID string
	var rowVersion int64
	err = tx.QueryRowContext(ctx, `SELECT id,row_version,COALESCE(current_revision_id,'') FROM llm_skill WHERE namespace_id=? AND (id=? OR name=?)`, item.Namespace, item.ID, item.Name).Scan(&id, &rowVersion, &currentRevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		id = item.ID
		rowVersion = 1
		if _, err = tx.ExecContext(ctx, `INSERT INTO llm_skill(id,namespace_id,name,description,status,created_by,updated_by,metadata) VALUES(?,?,?,?,'ACTIVE',?,?,?)`, id, item.Namespace, item.Name, item.Description, principal, principal, string(jsonValue(item.Metadata))); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM llm_skill_revision WHERE skill_id=?`, id).Scan(&revision); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO llm_skill_revision(id,namespace_id,skill_id,revision,instruction,model_config,tools,description,status,created_by,updated_by) VALUES(?,?,?,?,?,?,?,?, 'PUBLISHED',?,?)`, revisionID, item.Namespace, id, revision, item.Instruction, string(jsonValue(skillModelConfig(item))), string(jsonValue(skillTools(item))), item.Description, principal, principal); err != nil {
		return err
	}
	if err = cloneSkillFilesSQLite(ctx, tx, currentRevisionID, revisionID, item.Namespace, "", ""); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE llm_skill SET name=?,description=?,current_revision_id=?,status='ACTIVE',updated_by=?,metadata=? WHERE id=? AND namespace_id=? AND row_version=?`, item.Name, item.Description, revisionID, principal, string(jsonValue(item.Metadata)), id, item.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("skill revision CAS conflict")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item.ID = id
	item.Revision = revision
	item.Version = revision
	return nil
}

func (s *SkillSQLiteStore) SaveFileRevision(ctx context.Context, namespace, name string, file entities.SkillFile, principal string) (*entities.SkillInfo, error) {
	if principal == "" {
		principal = "system:flowgent"
	}
	if err := validateSkillFile(file); err != nil {
		return nil, err
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var skillID, currentRevisionID, instruction, modelConfig, tools, description string
	var rowVersion, revision int64
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.row_version,s.current_revision_id,r.revision,r.instruction,r.model_config,r.tools,COALESCE(r.description,'') FROM llm_skill s JOIN llm_skill_revision r ON r.id=s.current_revision_id WHERE s.namespace_id=? AND s.name=? AND s.status<>'DELETED'`, namespace, name).Scan(&skillID, &rowVersion, &currentRevisionID, &revision, &instruction, &modelConfig, &tools, &description)
	if err != nil {
		return nil, err
	}
	newRevisionID := uuid.NewString()
	newRevision := revision + 1
	if _, err = tx.ExecContext(ctx, `INSERT INTO llm_skill_revision(id,namespace_id,skill_id,revision,instruction,model_config,tools,description,status,created_by,updated_by) VALUES(?,?,?,?,?,?,?,?, 'PUBLISHED',?,?)`, newRevisionID, namespace, skillID, newRevision, instruction, modelConfig, tools, description, principal, principal); err != nil {
		return nil, err
	}
	if err = cloneSkillFilesSQLite(ctx, tx, currentRevisionID, newRevisionID, namespace, file.Kind, file.RelativePath); err != nil {
		return nil, err
	}
	if file.ID == "" {
		file.ID = uuid.NewString()
	}
	if file.CreatedAt.IsZero() {
		file.CreatedAt = time.Now().UTC()
	}
	if file.CreatedBy == "" {
		file.CreatedBy = principal
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO llm_skill_file(id,namespace_id,skill_revision_id,kind,relative_path,media_type,size_bytes,content_hash,created_at,created_by,metadata) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, file.ID, namespace, newRevisionID, file.Kind, file.RelativePath, file.MediaType, file.SizeBytes, file.ContentHash, file.CreatedAt, file.CreatedBy, string(jsonValue(file.Metadata))); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE llm_skill SET current_revision_id=?,updated_by=? WHERE id=? AND namespace_id=? AND row_version=?`, newRevisionID, principal, skillID, namespace, rowVersion)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("skill file revision CAS conflict")
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, namespace, name)
}

func (s *SkillSQLiteStore) Delete(ctx context.Context, namespace, name string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").SQLiteWhere()
	args := append([]any{namespace, name}, scopeArgs...)
	result, err := s.conn.ExecContext(ctx, `UPDATE llm_skill SET status='DELETED' WHERE namespace_id=?1 AND name=?2 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("skill not found or outside authorization scope")
	}
	return nil
}

func cloneSkillFilesSQLite(ctx context.Context, tx *sql.Tx, fromRevisionID, toRevisionID, namespace, replaceKind, replacePath string) error {
	if fromRevisionID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+utils.Columns[skillFileRecord]()+` FROM llm_skill_file WHERE skill_revision_id=? ORDER BY kind,relative_path`, fromRevisionID)
	if err != nil {
		return err
	}
	records := make([]skillFileRecord, 0)
	for rows.Next() {
		var record skillFileRecord
		if err := utils.ScanStruct(rows, &record); err != nil {
			rows.Close()
			return err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, record := range records {
		if record.Kind == replaceKind && record.RelativePath == replacePath {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO llm_skill_file(id,namespace_id,skill_revision_id,kind,relative_path,media_type,size_bytes,content_hash,created_at,created_by,metadata) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, uuid.NewString(), namespace, toRevisionID, record.Kind, record.RelativePath, record.MediaType, record.SizeBytes, record.ContentHash, record.CreatedAt, record.CreatedBy, string(jsonValue(record.Metadata))); err != nil {
			return err
		}
	}
	return nil
}
