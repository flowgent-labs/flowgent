package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	"github.com/flowgent-labs/flowgent/storage/pkg"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SkillPostgresStore struct{ pool *pgxpool.Pool }

func NewSkillPostgresStore(pool *pgxpool.Pool) *SkillPostgresStore {
	return &SkillPostgresStore{pool: pool}
}

func (s *SkillPostgresStore) Get(ctx context.Context, namespace, name string) (*entities.SkillInfo, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").PostgresWhere(3)
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[skillRecord]() + ` FROM (SELECT a.name,r.revision,r.instruction,r.model_config,r.tools,a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata FROM (SELECT * FROM llm_skill WHERE namespace_id=$1 AND name=$2 AND status<>'DELETED' AND (` + scopeWhere + `)) a JOIN llm_skill_revision r ON r.id=a.current_revision_id) current_skill`
	var record skillRecord
	if err := utils.ScanStruct(s.pool.QueryRow(ctx, query, args...), &record); err != nil {
		return nil, err
	}
	return record.entity(), nil
}

func (s *SkillPostgresStore) List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.SkillInfo], error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 {
		req.Size = 20
	}
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").PostgresWhere(2)
	baseArgs := append([]any{namespace}, scopeArgs...)
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM llm_skill WHERE namespace_id=$1 AND status<>'DELETED' AND (`+scopeWhere+`)`, baseArgs...).Scan(&total); err != nil {
		return nil, err
	}
	pos := len(baseArgs) + 1
	args := append(baseArgs, req.Size, (req.Page-1)*req.Size)
	query := fmt.Sprintf(`SELECT %s FROM (SELECT a.name,r.revision,r.instruction,r.model_config,r.tools,a.id,a.description,a.namespace_id,a.status,a.created_at,a.created_by,a.updated_at,a.updated_by,a.row_version,a.metadata FROM (SELECT * FROM llm_skill WHERE namespace_id=$1 AND status<>'DELETED' AND (%s)) a JOIN llm_skill_revision r ON r.id=a.current_revision_id) current_skill ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, utils.Columns[skillRecord](), scopeWhere, pos, pos+1)
	rows, err := s.pool.Query(ctx, query, args...)
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

func (s *SkillPostgresStore) ListFiles(ctx context.Context, namespace, name string) ([]entities.SkillFile, error) {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").PostgresWhere(3)
	args := append([]any{namespace, name}, scopeArgs...)
	query := `SELECT ` + utils.Columns[skillFileRecord]() + ` FROM (SELECT f.id,f.kind,f.relative_path,f.media_type,f.size_bytes,f.content_hash,f.created_at,f.created_by,f.metadata FROM llm_skill_file f JOIN (SELECT * FROM llm_skill WHERE namespace_id=$1 AND name=$2 AND status<>'DELETED' AND (` + scopeWhere + `)) s ON s.current_revision_id=f.skill_revision_id) current_skill_files ORDER BY kind,relative_path`
	rows, err := s.pool.Query(ctx, query, args...)
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

func (s *SkillPostgresStore) Save(ctx context.Context, item *entities.SkillInfo) error {
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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO orh_namespace(id,name,description,created_by,updated_by) VALUES($1::varchar(64),$1::text,'Flowgent namespace',$2::varchar(255),$2::varchar(255)) ON CONFLICT(id) DO NOTHING`, item.Namespace, principal); err != nil {
		return err
	}
	var id, currentRevisionID string
	var rowVersion int64
	err = tx.QueryRow(ctx, `SELECT id,row_version,COALESCE(current_revision_id,'') FROM llm_skill WHERE namespace_id=$1 AND (id=$2 OR name=$3) FOR UPDATE`, item.Namespace, item.ID, item.Name).Scan(&id, &rowVersion, &currentRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		id = item.ID
		rowVersion = 1
		if _, err = tx.Exec(ctx, `INSERT INTO llm_skill(id,namespace_id,name,description,status,created_by,updated_by,metadata) VALUES($1,$2,$3,$4,'ACTIVE',$5,$5,$6)`, id, item.Namespace, item.Name, item.Description, principal, jsonValue(item.Metadata)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM llm_skill_revision WHERE skill_id=$1`, id).Scan(&revision); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err = tx.Exec(ctx, `INSERT INTO llm_skill_revision(id,namespace_id,skill_id,revision,instruction,model_config,tools,description,status,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PUBLISHED',$9,$9)`, revisionID, item.Namespace, id, revision, item.Instruction, jsonValue(skillModelConfig(item)), jsonValue(skillTools(item)), item.Description, principal); err != nil {
		return err
	}
	if err = cloneSkillFilesPostgres(ctx, tx, currentRevisionID, revisionID, item.Namespace, "", ""); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE llm_skill SET name=$1,description=$2,current_revision_id=$3,status='ACTIVE',updated_by=$4,metadata=$5 WHERE id=$6 AND namespace_id=$7 AND row_version=$8`, item.Name, item.Description, revisionID, principal, jsonValue(item.Metadata), id, item.Namespace, rowVersion)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("skill revision CAS conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	item.ID = id
	item.Revision = revision
	item.Version = revision
	return nil
}

func (s *SkillPostgresStore) SaveFileRevision(ctx context.Context, namespace, name string, file entities.SkillFile, principal string) (*entities.SkillInfo, error) {
	if principal == "" {
		principal = "system:flowgent"
	}
	if err := validateSkillFile(file); err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var skillID, currentRevisionID, instruction, description string
	var rowVersion, revision int64
	var modelConfig, tools []byte
	err = tx.QueryRow(ctx, `SELECT s.id,s.row_version,s.current_revision_id,r.revision,r.instruction,r.model_config,r.tools,COALESCE(r.description,'') FROM llm_skill s JOIN llm_skill_revision r ON r.id=s.current_revision_id WHERE s.namespace_id=$1 AND s.name=$2 AND s.status<>'DELETED' FOR UPDATE OF s`, namespace, name).Scan(&skillID, &rowVersion, &currentRevisionID, &revision, &instruction, &modelConfig, &tools, &description)
	if err != nil {
		return nil, err
	}
	newRevisionID := uuid.NewString()
	newRevision := revision + 1
	if _, err = tx.Exec(ctx, `INSERT INTO llm_skill_revision(id,namespace_id,skill_id,revision,instruction,model_config,tools,description,status,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PUBLISHED',$9,$9)`, newRevisionID, namespace, skillID, newRevision, instruction, modelConfig, tools, description, principal); err != nil {
		return nil, err
	}
	if err = cloneSkillFilesPostgres(ctx, tx, currentRevisionID, newRevisionID, namespace, file.Kind, file.RelativePath); err != nil {
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
	if _, err = tx.Exec(ctx, `INSERT INTO llm_skill_file(id,namespace_id,skill_revision_id,kind,relative_path,media_type,size_bytes,content_hash,created_at,created_by,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, file.ID, namespace, newRevisionID, file.Kind, file.RelativePath, file.MediaType, file.SizeBytes, file.ContentHash, file.CreatedAt, file.CreatedBy, jsonValue(file.Metadata)); err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx, `UPDATE llm_skill SET current_revision_id=$1,updated_by=$2 WHERE id=$3 AND namespace_id=$4 AND row_version=$5`, newRevisionID, principal, skillID, namespace, rowVersion)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, fmt.Errorf("skill file revision CAS conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, namespace, name)
}

func (s *SkillPostgresStore) Delete(ctx context.Context, namespace, name string) error {
	scopeWhere, scopeArgs := storage.FlowgentSqlScopeForTable(ctx, "llm_skill").PostgresWhere(3)
	args := append([]any{namespace, name}, scopeArgs...)
	result, err := s.pool.Exec(ctx, `UPDATE llm_skill SET status='DELETED' WHERE namespace_id=$1 AND name=$2 AND status<>'DELETED' AND (`+scopeWhere+`)`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("skill not found or outside authorization scope")
	}
	return nil
}

func cloneSkillFilesPostgres(ctx context.Context, tx pgx.Tx, fromRevisionID, toRevisionID, namespace, replaceKind, replacePath string) error {
	if fromRevisionID == "" {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT `+utils.Columns[skillFileRecord]()+` FROM llm_skill_file WHERE skill_revision_id=$1 ORDER BY kind,relative_path`, fromRevisionID)
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
		if _, err := tx.Exec(ctx, `INSERT INTO llm_skill_file(id,namespace_id,skill_revision_id,kind,relative_path,media_type,size_bytes,content_hash,created_at,created_by,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid.NewString(), namespace, toRevisionID, record.Kind, record.RelativePath, record.MediaType, record.SizeBytes, record.ContentHash, record.CreatedAt, record.CreatedBy, jsonValue(record.Metadata)); err != nil {
			return err
		}
	}
	return nil
}

func validateSkillFile(file entities.SkillFile) error {
	if file.Kind != "asset" && file.Kind != "script" {
		return fmt.Errorf("skill file kind must be asset or script")
	}
	if file.RelativePath == "" || strings.HasPrefix(file.RelativePath, "/") || strings.Contains("/"+file.RelativePath+"/", "/../") {
		return fmt.Errorf("invalid skill file relative path")
	}
	if file.MediaType == "" || file.SizeBytes < 0 {
		return fmt.Errorf("invalid skill file metadata")
	}
	if len(file.ContentHash) != 64 {
		return fmt.Errorf("invalid skill file content hash")
	}
	for _, char := range file.ContentHash {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return fmt.Errorf("invalid skill file content hash")
		}
	}
	return nil
}

func jsonValue(value any) []byte { data, _ := json.Marshal(value); return data }
