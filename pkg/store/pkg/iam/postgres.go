package iam

import (
	"context"
	"fmt"
	"strings"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct {
	pool       *pgxpool.Pool
	namespaces *storepkg.PostgresGenericStore[entities.IAMNamespace]
	principals *storepkg.PostgresGenericStore[entities.IAMPrincipal]
	groups     *storepkg.PostgresGenericStore[entities.IAMGroup]
	members    *storepkg.PostgresGenericStore[entities.IAMGroupMember]
	roles      *storepkg.PostgresGenericStore[entities.IAMRole]
	bindings   *storepkg.PostgresGenericStore[entities.IAMRoleBinding]
	apiKeys    *storepkg.PostgresGenericStore[entities.IAMAPIKey]
	audits     *storepkg.PostgresGenericStore[entities.IAMAuditEvent]
}

func newPostgresRepository(pool *pgxpool.Pool) *postgresRepository {
	return &postgresRepository{
		pool:       pool,
		namespaces: pgStore[entities.IAMNamespace](pool, "iam_namespace"),
		principals: pgStore[entities.IAMPrincipal](pool, "iam_principal"),
		groups:     pgStore[entities.IAMGroup](pool, "iam_group"),
		members:    pgStore[entities.IAMGroupMember](pool, "iam_group_member"),
		roles:      pgStore[entities.IAMRole](pool, "iam_role"),
		bindings:   pgStore[entities.IAMRoleBinding](pool, "iam_role_binding"),
		apiKeys:    pgStore[entities.IAMAPIKey](pool, "iam_api_key"),
		audits:     pgStore[entities.IAMAuditEvent](pool, "iam_audit_event"),
	}
}

func (s *postgresRepository) GetNamespace(ctx context.Context, id string) (*entities.IAMNamespace, error) {
	return s.namespaces.Get(ctx, id)
}
func (s *postgresRepository) ListNamespaces(ctx context.Context) ([]*entities.IAMNamespace, error) {
	return pgList[entities.IAMNamespace](ctx, s.pool, "iam_namespace", "status='ACTIVE'")
}
func (s *postgresRepository) SaveNamespace(ctx context.Context, item *entities.IAMNamespace) error {
	return s.namespaces.Save(ctx, item)
}
func (s *postgresRepository) DeleteNamespace(ctx context.Context, id string) error {
	return s.namespaces.Delete(ctx, id)
}

func pgStore[T any](pool *pgxpool.Pool, table string) *storepkg.PostgresGenericStore[T] {
	return &storepkg.PostgresGenericStore[T]{Pool: pool, Table: table, IDCol: "id"}
}

func pgList[T any](ctx context.Context, pool *pgxpool.Pool, table, where string, args ...any) ([]*T, error) {
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE del_flag=false`, utils.Columns[T](), table)
	if where != "" {
		query += " AND " + where
	}
	query += " ORDER BY created_at DESC"
	rows, err := pool.Query(ctx, query, args...)
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

func (s *postgresRepository) GetPrincipal(ctx context.Context, id string) (*entities.IAMPrincipal, error) {
	return s.principals.Get(ctx, id)
}
func (s *postgresRepository) FindPrincipalByIdentity(ctx context.Context, issuer, externalID string) (*entities.IAMPrincipal, error) {
	query := fmt.Sprintf(`SELECT %s FROM iam_principal WHERE issuer=$1 AND external_id=$2 AND del_flag=false AND status='ACTIVE'`, utils.Columns[entities.IAMPrincipal]())
	rows, err := s.pool.Query(ctx, query, issuer, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("principal not found")
	}
	var principal entities.IAMPrincipal
	if err := utils.ScanStruct(rows, &principal); err != nil {
		return nil, err
	}
	return &principal, nil
}
func (s *postgresRepository) ListPrincipals(ctx context.Context) ([]*entities.IAMPrincipal, error) {
	return pgList[entities.IAMPrincipal](ctx, s.pool, "iam_principal", "")
}
func (s *postgresRepository) ListNamespacePrincipals(ctx context.Context, namespace string) ([]*entities.IAMPrincipal, error) {
	return pgList[entities.IAMPrincipal](ctx, s.pool, "iam_principal", `id IN (
		SELECT principal_id FROM iam_namespace_member WHERE del_flag=false AND status='ACTIVE' AND namespace_id=$1
	)`, namespace)
}
func (s *postgresRepository) SavePrincipal(ctx context.Context, item *entities.IAMPrincipal) error {
	return s.principals.Save(ctx, item)
}
func (s *postgresRepository) DeletePrincipal(ctx context.Context, id string) error {
	return s.principals.Delete(ctx, id)
}
func (s *postgresRepository) GetNamespaceMember(ctx context.Context, namespace, principalID string) (*entities.IAMNamespaceMember, error) {
	items, err := pgList[entities.IAMNamespaceMember](ctx, s.pool, "iam_namespace_member", "namespace_id=$1 AND principal_id=$2", namespace, principalID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return items[0], nil
}
func (s *postgresRepository) SaveNamespaceMember(ctx context.Context, item *entities.IAMNamespaceMember) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO iam_namespace_member
		(id,principal_id,membership,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT(namespace_id,principal_id) DO UPDATE SET
			membership=EXCLUDED.membership,description=EXCLUDED.description,status='ACTIVE',
			updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,del_flag=false`,
		item.ID, item.PrincipalID, item.Membership, item.Description, item.Namespace, item.Status,
		item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy, item.DelFlag)
	return err
}
func (s *postgresRepository) RemoveNamespaceMember(ctx context.Context, namespace, principalID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`UPDATE iam_group_member SET del_flag=true,status='DELETED',updated_at=NOW() WHERE namespace_id=$1 AND principal_id=$2 AND del_flag=false`,
		`UPDATE iam_role_binding SET del_flag=true,status='DELETED',updated_at=NOW() WHERE namespace_id=$1 AND subject_id=$2 AND subject_type IN ('user','service_account') AND del_flag=false`,
		`UPDATE iam_namespace_member SET del_flag=true,status='DELETED',updated_at=NOW() WHERE namespace_id=$1 AND principal_id=$2 AND del_flag=false`,
	} {
		if _, err := tx.Exec(ctx, statement, namespace, principalID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *postgresRepository) GetGroup(ctx context.Context, id string) (*entities.IAMGroup, error) {
	return s.groups.Get(ctx, id)
}
func (s *postgresRepository) ListGroups(ctx context.Context, namespace string) ([]*entities.IAMGroup, error) {
	return pgList[entities.IAMGroup](ctx, s.pool, "iam_group", "namespace_id=$1", namespace)
}
func (s *postgresRepository) SaveGroup(ctx context.Context, item *entities.IAMGroup) error {
	return s.groups.Save(ctx, item)
}
func (s *postgresRepository) DeleteGroup(ctx context.Context, id string) error {
	return s.groups.Delete(ctx, id)
}
func (s *postgresRepository) SaveGroupMember(ctx context.Context, item *entities.IAMGroupMember) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO iam_group_member
		(id,group_id,principal_id,description,namespace_id,status,created_at,created_by,updated_at,updated_by,del_flag)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT(group_id,principal_id) DO UPDATE SET
			description=EXCLUDED.description,status='ACTIVE',updated_at=EXCLUDED.updated_at,
			updated_by=EXCLUDED.updated_by,del_flag=false`,
		item.ID, item.GroupID, item.PrincipalID, item.Description, item.Namespace, item.Status,
		item.CreatedAt, item.CreatedBy, item.UpdatedAt, item.UpdatedBy, item.DelFlag)
	return err
}
func (s *postgresRepository) DeleteGroupMember(ctx context.Context, id string) error {
	return s.members.Delete(ctx, id)
}
func (s *postgresRepository) ListGroupMembers(ctx context.Context, namespace, groupID string) ([]*entities.IAMGroupMember, error) {
	return pgList[entities.IAMGroupMember](ctx, s.pool, "iam_group_member", "namespace_id=$1 AND group_id=$2", namespace, groupID)
}
func (s *postgresRepository) ListPrincipalGroups(ctx context.Context, namespace, principalID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.id,g.name FROM iam_group_member m JOIN iam_group g ON g.id=m.group_id
        WHERE m.del_flag=false AND g.del_flag=false AND m.status='ACTIVE' AND g.status='ACTIVE'
          AND m.namespace_id=$1 AND m.principal_id=$2`, namespace, principalID)
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

func (s *postgresRepository) GetRole(ctx context.Context, id string) (*entities.IAMRole, error) {
	return s.roles.Get(ctx, id)
}
func (s *postgresRepository) ListRoles(ctx context.Context, namespace string) ([]*entities.IAMRole, error) {
	return pgList[entities.IAMRole](ctx, s.pool, "iam_role", "namespace_id IN ($1,'*')", namespace)
}
func (s *postgresRepository) SaveRole(ctx context.Context, item *entities.IAMRole) error {
	return s.roles.Save(ctx, item)
}
func (s *postgresRepository) DeleteRole(ctx context.Context, id string) error {
	return s.roles.Delete(ctx, id)
}

func (s *postgresRepository) GetBinding(ctx context.Context, id string) (*entities.IAMRoleBinding, error) {
	return s.bindings.Get(ctx, id)
}
func (s *postgresRepository) ListBindings(ctx context.Context, namespace string) ([]*entities.IAMRoleBinding, error) {
	return pgList[entities.IAMRoleBinding](ctx, s.pool, "iam_role_binding", "namespace_id=$1", namespace)
}
func (s *postgresRepository) FindBindings(ctx context.Context, namespace string, principalType entities.PrincipalType, principalID string, groupIDs []string) ([]*entities.IAMRoleBinding, error) {
	if len(groupIDs) == 0 {
		groupIDs = []string{""}
	}
	return pgList[entities.IAMRoleBinding](ctx, s.pool, "iam_role_binding", `namespace_id IN ($1,'*') AND status='ACTIVE'
        AND ((subject_type=$2 AND subject_id=$3) OR (subject_type='group' AND subject_id=ANY($4)))`,
		namespace, string(principalType), principalID, groupIDs)
}
func (s *postgresRepository) SaveBinding(ctx context.Context, item *entities.IAMRoleBinding) error {
	item.Effect = strings.ToUpper(item.Effect)
	return s.bindings.Save(ctx, item)
}
func (s *postgresRepository) DeleteBinding(ctx context.Context, id string) error {
	return s.bindings.Delete(ctx, id)
}

func (s *postgresRepository) GetAPIKey(ctx context.Context, id string) (*entities.IAMAPIKey, error) {
	return s.apiKeys.Get(ctx, id)
}
func (s *postgresRepository) ListAPIKeys(ctx context.Context, namespace string) ([]*entities.IAMAPIKey, error) {
	return pgList[entities.IAMAPIKey](ctx, s.pool, "iam_api_key", `allowed_namespaces @> to_jsonb(ARRAY[$1]::text[]) OR allowed_namespaces @> '["*"]'::jsonb`, namespace)
}
func (s *postgresRepository) SaveAPIKey(ctx context.Context, item *entities.IAMAPIKey) error {
	return s.apiKeys.Save(ctx, item)
}
func (s *postgresRepository) DeleteAPIKey(ctx context.Context, id string) error {
	return s.apiKeys.Delete(ctx, id)
}

func (s *postgresRepository) AppendAudit(ctx context.Context, item *entities.IAMAuditEvent) error {
	return s.audits.Save(ctx, item)
}
func (s *postgresRepository) ListAudit(ctx context.Context, namespace string, limit int) ([]*entities.IAMAuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := fmt.Sprintf(`SELECT %s FROM iam_audit_event WHERE del_flag=false AND namespace_id IN ($1,'') ORDER BY created_at DESC LIMIT $2`, utils.Columns[entities.IAMAuditEvent]())
	rows, err := s.pool.Query(ctx, query, namespace, limit)
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
