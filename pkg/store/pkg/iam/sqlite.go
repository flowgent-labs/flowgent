package iam

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

type sqliteRepository struct {
	db         *sql.DB
	namespaces *storepkg.SQLiteGenericStore[entities.IAMNamespace]
	principals *storepkg.SQLiteGenericStore[entities.IAMPrincipal]
	groups     *storepkg.SQLiteGenericStore[entities.IAMGroup]
	members    *storepkg.SQLiteGenericStore[entities.IAMGroupMember]
	roles      *storepkg.SQLiteGenericStore[entities.IAMRole]
	bindings   *storepkg.SQLiteGenericStore[entities.IAMRoleBinding]
	apiKeys    *storepkg.SQLiteGenericStore[entities.IAMAPIKey]
	audits     *storepkg.SQLiteGenericStore[entities.IAMAuditEvent]
}

func newSQLiteRepository(db *sql.DB) *sqliteRepository {
	return &sqliteRepository{
		db:         db,
		namespaces: sqStore[entities.IAMNamespace](db, "iam_namespace"),
		principals: sqStore[entities.IAMPrincipal](db, "iam_principal"),
		groups:     sqStore[entities.IAMGroup](db, "iam_group"),
		members:    sqStore[entities.IAMGroupMember](db, "iam_group_member"),
		roles:      sqStore[entities.IAMRole](db, "iam_role"),
		bindings:   sqStore[entities.IAMRoleBinding](db, "iam_role_binding"),
		apiKeys:    sqStore[entities.IAMAPIKey](db, "iam_api_key"),
		audits:     sqStore[entities.IAMAuditEvent](db, "iam_audit_event"),
	}
}

func (s *sqliteRepository) GetNamespace(ctx context.Context, id string) (*entities.IAMNamespace, error) {
	return s.namespaces.Get(ctx, id)
}
func (s *sqliteRepository) ListNamespaces(ctx context.Context) ([]*entities.IAMNamespace, error) {
	return sqList[entities.IAMNamespace](ctx, s.db, "iam_namespace", "status='ACTIVE'")
}
func (s *sqliteRepository) SaveNamespace(ctx context.Context, item *entities.IAMNamespace) error {
	return s.namespaces.Save(ctx, item)
}
func (s *sqliteRepository) DeleteNamespace(ctx context.Context, id string) error {
	return s.namespaces.Delete(ctx, id)
}

func sqStore[T any](db *sql.DB, table string) *storepkg.SQLiteGenericStore[T] {
	return &storepkg.SQLiteGenericStore[T]{Conn: db, Table: table, IDCol: "id"}
}

func sqList[T any](ctx context.Context, db *sql.DB, table, where string, args ...any) ([]*T, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE del_flag=0`, utils.Columns[T](), table)
	if where != "" {
		query += " AND " + where
	}
	query += " ORDER BY created_at DESC"
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*T, 0)
	for rows.Next() {
		item := new(T)
		if err := utils.ScanStruct(rows, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqliteRepository) GetPrincipal(ctx context.Context, id string) (*entities.IAMPrincipal, error) {
	return s.principals.Get(ctx, id)
}
func (s *sqliteRepository) FindPrincipalByIdentity(ctx context.Context, issuer, externalID string) (*entities.IAMPrincipal, error) {
	query := fmt.Sprintf(`SELECT %s FROM iam_principal WHERE issuer=? AND external_id=? AND del_flag=0 AND status='ACTIVE'`, utils.Columns[entities.IAMPrincipal]())
	row := s.db.QueryRowContext(ctx, query, issuer, externalID)
	var principal entities.IAMPrincipal
	if err := utils.ScanStruct(row, &principal); err != nil {
		return nil, err
	}
	return &principal, nil
}
func (s *sqliteRepository) ListPrincipals(ctx context.Context) ([]*entities.IAMPrincipal, error) {
	return sqList[entities.IAMPrincipal](ctx, s.db, "iam_principal", "")
}
func (s *sqliteRepository) ListNamespacePrincipals(ctx context.Context, namespace string) ([]*entities.IAMPrincipal, error) {
	return sqList[entities.IAMPrincipal](ctx, s.db, "iam_principal", `id IN (
		SELECT principal_id FROM iam_namespace_member WHERE del_flag=0 AND status='ACTIVE' AND namespace_id=?
	)`, namespace)
}
func (s *sqliteRepository) SavePrincipal(ctx context.Context, item *entities.IAMPrincipal) error {
	return s.principals.Save(ctx, item)
}
func (s *sqliteRepository) DeletePrincipal(ctx context.Context, id string) error {
	return s.principals.Delete(ctx, id)
}
func (s *sqliteRepository) GetNamespaceMember(ctx context.Context, namespace, principalID string) (*entities.IAMNamespaceMember, error) {
	items, err := sqList[entities.IAMNamespaceMember](ctx, s.db, "iam_namespace_member", "namespace_id=? AND principal_id=?", namespace, principalID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return items[0], nil
}
func (s *sqliteRepository) SaveNamespaceMember(ctx context.Context, item *entities.IAMNamespaceMember) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO iam_namespace_member
		(id,principal_id,membership,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(namespace_id,principal_id) DO UPDATE SET
			membership=excluded.membership,description=excluded.description,status='ACTIVE',
			updated_at=excluded.updated_at,updated_by=excluded.updated_by,del_flag=0`,
		item.ID, item.PrincipalID, item.Membership, item.Description, item.Namespace, item.Status,
		sqliteIAMTime(item.CreatedAt), item.CreatedBy, sqliteIAMTime(item.UpdatedAt), item.UpdatedBy, item.DelFlag)
	return err
}
func (s *sqliteRepository) RemoveNamespaceMember(ctx context.Context, namespace, principalID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, statement := range []string{
		`UPDATE iam_group_member SET del_flag=1,status='DELETED',updated_at=? WHERE namespace_id=? AND principal_id=? AND del_flag=0`,
		`UPDATE iam_role_binding SET del_flag=1,status='DELETED',updated_at=? WHERE namespace_id=? AND subject_id=? AND subject_type IN ('user','service_account') AND del_flag=0`,
		`UPDATE iam_namespace_member SET del_flag=1,status='DELETED',updated_at=? WHERE namespace_id=? AND principal_id=? AND del_flag=0`,
	} {
		if _, err := tx.ExecContext(ctx, statement, now, namespace, principalID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteRepository) GetGroup(ctx context.Context, id string) (*entities.IAMGroup, error) {
	return s.groups.Get(ctx, id)
}
func (s *sqliteRepository) ListGroups(ctx context.Context, namespace string) ([]*entities.IAMGroup, error) {
	return sqList[entities.IAMGroup](ctx, s.db, "iam_group", "namespace_id=?", namespace)
}
func (s *sqliteRepository) SaveGroup(ctx context.Context, item *entities.IAMGroup) error {
	return s.groups.Save(ctx, item)
}
func (s *sqliteRepository) DeleteGroup(ctx context.Context, id string) error {
	return s.groups.Delete(ctx, id)
}
func (s *sqliteRepository) SaveGroupMember(ctx context.Context, item *entities.IAMGroupMember) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO iam_group_member
		(id,group_id,principal_id,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(group_id,principal_id) DO UPDATE SET
			description=excluded.description,status='ACTIVE',updated_at=excluded.updated_at,
			updated_by=excluded.updated_by,del_flag=0`,
		item.ID, item.GroupID, item.PrincipalID, item.Description, item.Namespace, item.Status,
		sqliteIAMTime(item.CreatedAt), item.CreatedBy, sqliteIAMTime(item.UpdatedAt), item.UpdatedBy, item.DelFlag)
	return err
}

func sqliteIAMTime(value time.Time) string {
	return value.Format("2006-01-02 15:04:05")
}
func (s *sqliteRepository) DeleteGroupMember(ctx context.Context, id string) error {
	return s.members.Delete(ctx, id)
}
func (s *sqliteRepository) ListGroupMembers(ctx context.Context, namespace, groupID string) ([]*entities.IAMGroupMember, error) {
	return sqList[entities.IAMGroupMember](ctx, s.db, "iam_group_member", "namespace_id=? AND group_id=?", namespace, groupID)
}
func (s *sqliteRepository) ListPrincipalGroups(ctx context.Context, namespace, principalID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.id,g.name FROM iam_group_member m JOIN iam_group g ON g.id=m.group_id
        WHERE m.del_flag=0 AND g.del_flag=0 AND m.status='ACTIVE' AND g.status='ACTIVE'
          AND m.namespace_id=? AND m.principal_id=?`, namespace, principalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		groups = append(groups, id, name)
	}
	return groups, rows.Err()
}

func (s *sqliteRepository) GetRole(ctx context.Context, id string) (*entities.IAMRole, error) {
	return s.roles.Get(ctx, id)
}
func (s *sqliteRepository) ListRoles(ctx context.Context, namespace string) ([]*entities.IAMRole, error) {
	return sqList[entities.IAMRole](ctx, s.db, "iam_role", "namespace_id IN (?,'*')", namespace)
}
func (s *sqliteRepository) SaveRole(ctx context.Context, item *entities.IAMRole) error {
	return s.roles.Save(ctx, item)
}
func (s *sqliteRepository) DeleteRole(ctx context.Context, id string) error {
	return s.roles.Delete(ctx, id)
}

func (s *sqliteRepository) GetBinding(ctx context.Context, id string) (*entities.IAMRoleBinding, error) {
	return s.bindings.Get(ctx, id)
}
func (s *sqliteRepository) ListBindings(ctx context.Context, namespace string) ([]*entities.IAMRoleBinding, error) {
	return sqList[entities.IAMRoleBinding](ctx, s.db, "iam_role_binding", "namespace_id=?", namespace)
}
func (s *sqliteRepository) FindBindings(ctx context.Context, namespace string, principalType entities.PrincipalType, principalID string, groupIDs []string) ([]*entities.IAMRoleBinding, error) {
	args := []any{namespace, string(principalType), principalID}
	placeholders := make([]string, 0, len(groupIDs))
	for _, id := range groupIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	groupClause := "0"
	if len(placeholders) > 0 {
		groupClause = "subject_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	where := `namespace_id IN (?,'*') AND status='ACTIVE' AND ((subject_type=? AND subject_id=?) OR (subject_type='group' AND ` + groupClause + `))`
	return sqList[entities.IAMRoleBinding](ctx, s.db, "iam_role_binding", where, args...)
}
func (s *sqliteRepository) SaveBinding(ctx context.Context, item *entities.IAMRoleBinding) error {
	item.Effect = strings.ToUpper(item.Effect)
	return s.bindings.Save(ctx, item)
}
func (s *sqliteRepository) DeleteBinding(ctx context.Context, id string) error {
	return s.bindings.Delete(ctx, id)
}

func (s *sqliteRepository) GetAPIKey(ctx context.Context, id string) (*entities.IAMAPIKey, error) {
	return s.apiKeys.Get(ctx, id)
}
func (s *sqliteRepository) ListAPIKeys(ctx context.Context, namespace string) ([]*entities.IAMAPIKey, error) {
	items, err := sqList[entities.IAMAPIKey](ctx, s.db, "iam_api_key", "")
	if err != nil {
		return nil, err
	}
	filtered := make([]*entities.IAMAPIKey, 0, len(items))
	for _, item := range items {
		for _, allowed := range item.AllowedNamespaces {
			if allowed == "*" || allowed == namespace {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}
func (s *sqliteRepository) SaveAPIKey(ctx context.Context, item *entities.IAMAPIKey) error {
	return s.apiKeys.Save(ctx, item)
}
func (s *sqliteRepository) DeleteAPIKey(ctx context.Context, id string) error {
	return s.apiKeys.Delete(ctx, id)
}

func (s *sqliteRepository) AppendAudit(ctx context.Context, item *entities.IAMAuditEvent) error {
	return s.audits.Save(ctx, item)
}
func (s *sqliteRepository) ListAudit(ctx context.Context, namespace string, limit int) ([]*entities.IAMAuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := fmt.Sprintf(`SELECT %s FROM iam_audit_event WHERE del_flag=0 AND namespace_id IN (?,'') ORDER BY created_at DESC LIMIT ?`, utils.Columns[entities.IAMAuditEvent]())
	rows, err := s.db.QueryContext(ctx, query, namespace, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*entities.IAMAuditEvent, 0)
	for rows.Next() {
		item := new(entities.IAMAuditEvent)
		if err := utils.ScanStruct(rows, item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
