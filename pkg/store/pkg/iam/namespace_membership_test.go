package iam

import (
	"context"
	"testing"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	storepkg "github.com/flowgent-labs/flowgent/store/pkg"
)

func TestNamespaceMembershipIsIndependentFromGlobalIdentity(t *testing.T) {
	ctx := context.Background()
	db := storepkg.NewSQLiteConn(ctx, t.TempDir())
	defer db.Close()
	repo := newSQLiteRepository(db)

	for _, id := range []string{"team-a", "team-b"} {
		namespace := &entities.IAMNamespace{
			BaseEntity: entities.BaseEntity{ID: id, Namespace: id},
			Name:       id,
			Labels:     map[string]string{},
		}
		namespace.MarkCreated("test")
		if err := repo.SaveNamespace(ctx, namespace); err != nil {
			t.Fatalf("SaveNamespace(%s): %v", id, err)
		}
	}
	principal := &entities.IAMPrincipal{
		BaseEntity: entities.BaseEntity{ID: "service:test-ci"},
		Type:       entities.PrincipalServiceAccount,
		Issuer:     "test",
		ExternalID: "test-ci",
		Username:   "test-ci",
		Attributes: map[string]any{},
	}
	principal.MarkCreated("test")
	if err := repo.SavePrincipal(ctx, principal); err != nil {
		t.Fatalf("SavePrincipal: %v", err)
	}

	saveMembership := func(id, namespace string) {
		t.Helper()
		member := &entities.IAMNamespaceMember{
			BaseEntity:  entities.BaseEntity{ID: id, Namespace: namespace},
			PrincipalID: principal.ID,
			Membership:  "MEMBER",
		}
		member.MarkCreated("test")
		if err := repo.SaveNamespaceMember(ctx, member); err != nil {
			t.Fatalf("SaveNamespaceMember(%s): %v", namespace, err)
		}
	}
	saveMembership("member-a", "team-a")
	saveMembership("member-b", "team-b")

	group := &entities.IAMGroup{
		BaseEntity: entities.BaseEntity{ID: "group-a", Namespace: "team-a"},
		Name:       "security",
	}
	group.MarkCreated("test")
	if err := repo.SaveGroup(ctx, group); err != nil {
		t.Fatal(err)
	}
	groupMember := &entities.IAMGroupMember{
		BaseEntity:  entities.BaseEntity{ID: "group-member-a", Namespace: "team-a"},
		GroupID:     group.ID,
		PrincipalID: principal.ID,
	}
	groupMember.MarkCreated("test")
	if err := repo.SaveGroupMember(ctx, groupMember); err != nil {
		t.Fatal(err)
	}
	binding := &entities.IAMRoleBinding{
		BaseEntity:   entities.BaseEntity{ID: "binding-a", Namespace: "team-a"},
		RoleID:       "builtin-reader",
		SubjectType:  entities.PrincipalServiceAccount,
		SubjectID:    principal.ID,
		ResourceType: "flow",
		ResourceID:   "security-autonomy-fixer",
		Effect:       "ALLOW",
		Conditions:   map[string]any{},
	}
	binding.MarkCreated("test")
	if err := repo.SaveBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}

	if err := repo.RemoveNamespaceMember(ctx, "team-a", principal.ID); err != nil {
		t.Fatalf("RemoveNamespaceMember: %v", err)
	}
	if _, err := repo.GetNamespaceMember(ctx, "team-a", principal.ID); err == nil {
		t.Fatal("removed team-a membership remained active")
	}
	if _, err := repo.GetNamespaceMember(ctx, "team-b", principal.ID); err != nil {
		t.Fatalf("team-b membership was affected: %v", err)
	}
	if _, err := repo.GetPrincipal(ctx, principal.ID); err != nil {
		t.Fatalf("global identity was deleted: %v", err)
	}
	if members, err := repo.ListGroupMembers(ctx, "team-a", group.ID); err != nil || len(members) != 0 {
		t.Fatalf("team-a group grants remain: members=%d err=%v", len(members), err)
	}
	if _, err := repo.GetBinding(ctx, binding.ID); err == nil {
		t.Fatal("team-a direct binding remained active")
	}

	// Re-inviting a previously removed identity revives the unique membership
	// edge without restoring any deleted grants.
	saveMembership("member-a-reinvite", "team-a")
	if member, err := repo.GetNamespaceMember(ctx, "team-a", principal.ID); err != nil || member.Status != "ACTIVE" {
		t.Fatalf("team-a membership was not revived: member=%+v err=%v", member, err)
	}
}
