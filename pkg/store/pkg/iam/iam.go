// Package iam persists deployment identities and namespace-scoped authorization
// policy. Authorization decisions remain in the API authz package; this package
// contains no HTTP or route knowledge.
package iam

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IRepository interface {
	GetNamespace(context.Context, string) (*entities.IAMNamespace, error)
	ListNamespaces(context.Context) ([]*entities.IAMNamespace, error)
	SaveNamespace(context.Context, *entities.IAMNamespace) error
	DeleteNamespace(context.Context, string) error

	GetPrincipal(context.Context, string) (*entities.IAMPrincipal, error)
	FindPrincipalByIdentity(context.Context, string, string) (*entities.IAMPrincipal, error)
	ListPrincipals(context.Context) ([]*entities.IAMPrincipal, error)
	ListNamespacePrincipals(context.Context, string) ([]*entities.IAMPrincipal, error)
	SavePrincipal(context.Context, *entities.IAMPrincipal) error
	DeletePrincipal(context.Context, string) error
	GetNamespaceMember(context.Context, string, string) (*entities.IAMNamespaceMember, error)
	SaveNamespaceMember(context.Context, *entities.IAMNamespaceMember) error
	RemoveNamespaceMember(context.Context, string, string) error

	GetGroup(context.Context, string) (*entities.IAMGroup, error)
	ListGroups(context.Context, string) ([]*entities.IAMGroup, error)
	SaveGroup(context.Context, *entities.IAMGroup) error
	DeleteGroup(context.Context, string) error
	ListPrincipalGroups(context.Context, string, string) ([]string, error)
	SaveGroupMember(context.Context, *entities.IAMGroupMember) error
	DeleteGroupMember(context.Context, string) error
	ListGroupMembers(context.Context, string, string) ([]*entities.IAMGroupMember, error)

	GetRole(context.Context, string) (*entities.IAMRole, error)
	ListRoles(context.Context, string) ([]*entities.IAMRole, error)
	SaveRole(context.Context, *entities.IAMRole) error
	DeleteRole(context.Context, string) error

	GetBinding(context.Context, string) (*entities.IAMRoleBinding, error)
	ListBindings(context.Context, string) ([]*entities.IAMRoleBinding, error)
	FindBindings(context.Context, string, entities.PrincipalType, string, []string) ([]*entities.IAMRoleBinding, error)
	SaveBinding(context.Context, *entities.IAMRoleBinding) error
	DeleteBinding(context.Context, string) error

	GetAPIKey(context.Context, string) (*entities.IAMAPIKey, error)
	ListAPIKeys(context.Context, string) ([]*entities.IAMAPIKey, error)
	SaveAPIKey(context.Context, *entities.IAMAPIKey) error
	DeleteAPIKey(context.Context, string) error

	AppendAudit(context.Context, *entities.IAMAuditEvent) error
	ListAudit(context.Context, string, int) ([]*entities.IAMAuditEvent, error)
}

func NewRepository(store storepkg.IStore) (IRepository, error) {
	switch db := store.DB().(type) {
	case *pgxpool.Pool:
		return newPostgresRepository(db), nil
	case *sql.DB:
		return newSQLiteRepository(db), nil
	default:
		return nil, fmt.Errorf("iam: unsupported store %T", store.DB())
	}
}
