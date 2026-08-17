package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/api/pkg/auth"
	"github.com/flowgent-labs/flowgent/api/pkg/authz"
	"github.com/flowgent-labs/flowgent/common/pkg/resourceid"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
	iamstore "github.com/flowgent-labs/flowgent/store/pkg/iam"
	"github.com/google/uuid"
)

const maxIAMBodyBytes = 1 << 20

type IAMHandler struct {
	repo       iamstore.IRepository
	authorizer *authz.Service
}

func NewIAMHandler(repo iamstore.IRepository, authorizer *authz.Service) *IAMHandler {
	return &IAMHandler{repo: repo, authorizer: authorizer}
}

func (h *IAMHandler) ListPrincipals(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListNamespacePrincipals(r.Context(), r.PathValue("namespace"))
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) GetPrincipal(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetPrincipal(r.Context(), r.PathValue("id"))
	if err == nil && !h.principalBelongsToNamespace(r, item.ID) {
		http.NotFound(w, r)
		return
	}
	writeIAMResult(w, item, err)
}

func (h *IAMHandler) CreatePrincipal(w http.ResponseWriter, r *http.Request) {
	if _, err := h.repo.GetNamespace(r.Context(), r.PathValue("namespace")); err != nil {
		http.NotFound(w, r)
		return
	}
	var item entities.IAMPrincipal
	if !decodeIAMBody(w, r, &item) {
		return
	}
	if item.Type == "" {
		item.Type = entities.PrincipalUser
	}
	if item.Type != entities.PrincipalUser && item.Type != entities.PrincipalServiceAccount {
		http.Error(w, "principal type must be user or service_account", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(item.ExternalID) == "" {
		http.Error(w, "external_id is required", http.StatusBadRequest)
		return
	}
	if item.Issuer == "" {
		item.Issuer = "flowgent:local"
	}
	existing, _ := h.repo.FindPrincipalByIdentity(r.Context(), item.Issuer, item.ExternalID)
	principal := &item
	if existing != nil {
		if existing.Type != item.Type {
			http.Error(w, "identity already exists with a different principal type", http.StatusConflict)
			return
		}
		principal = existing
	} else {
		if principal.ID == "" {
			principal.ID = uuid.NewString()
		}
		principal.Namespace = ""
		principal.MarkCreated(iamActor(r))
		if err := h.repo.SavePrincipal(r.Context(), principal); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}
	member := &entities.IAMNamespaceMember{
		BaseEntity:  entities.BaseEntity{ID: uuid.NewString(), Namespace: r.PathValue("namespace")},
		PrincipalID: principal.ID, Membership: "MEMBER",
	}
	member.MarkCreated(iamActor(r))
	if err := h.repo.SaveNamespaceMember(r.Context(), member); err != nil {
		http.Error(w, "identity is already a namespace member", http.StatusConflict)
		return
	}
	writeIAMCreated(w, principal)
}

func (h *IAMHandler) UpdatePrincipal(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetPrincipal(r.Context(), r.PathValue("id"))
	if err != nil || !h.principalBelongsToNamespace(r, item.ID) {
		http.NotFound(w, r)
		return
	}
	var update struct {
		Username    *string         `json:"username"`
		DisplayName *string         `json:"display_name"`
		Email       *string         `json:"email"`
		Description *string         `json:"description"`
		Status      *string         `json:"status"`
		Attributes  *map[string]any `json:"attributes"`
	}
	if !decodeIAMBody(w, r, &update) {
		return
	}
	if update.Username != nil {
		item.Username = *update.Username
	}
	if update.DisplayName != nil {
		item.DisplayName = *update.DisplayName
	}
	if update.Email != nil {
		item.Email = *update.Email
	}
	if update.Description != nil {
		item.Description = *update.Description
	}
	if update.Status != nil {
		item.Status = *update.Status
	}
	if update.Attributes != nil {
		item.Attributes = *update.Attributes
	}
	item.MarkUpdated(iamActor(r))
	if err := h.repo.SavePrincipal(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMResult(w, item, nil)
}

func (h *IAMHandler) DeletePrincipal(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetPrincipal(r.Context(), r.PathValue("id"))
	if err != nil || !h.principalBelongsToNamespace(r, item.ID) {
		http.NotFound(w, r)
		return
	}
	if item.ID == iamActor(r) {
		http.Error(w, "self-deletion is prohibited", http.StatusConflict)
		return
	}
	if h.isLastNamespaceOwner(r.Context(), r.PathValue("namespace"), item.ID, "") {
		http.Error(w, "the last namespace owner cannot be removed", http.StatusConflict)
		return
	}
	writeIAMNoContent(w, h.repo.RemoveNamespaceMember(r.Context(), r.PathValue("namespace"), item.ID))
}

func (h *IAMHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListGroups(r.Context(), r.PathValue("namespace"))
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetGroup(r.Context(), r.PathValue("id"))
	if err == nil && item.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	writeIAMResult(w, item, err)
}

func (h *IAMHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var item entities.IAMGroup
	if !decodeIAMBody(w, r, &item) {
		return
	}
	if strings.TrimSpace(item.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	item.ID = coalesceID(item.ID)
	item.Namespace = r.PathValue("namespace")
	item.MarkCreated(iamActor(r))
	if err := h.repo.SaveGroup(r.Context(), &item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMCreated(w, &item)
}

func (h *IAMHandler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetGroup(r.Context(), r.PathValue("id"))
	if err != nil || item.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	var update struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
	}
	if !decodeIAMBody(w, r, &update) {
		return
	}
	if update.Name != nil {
		item.Name = *update.Name
	}
	if update.Description != nil {
		item.Description = *update.Description
	}
	if update.Status != nil {
		item.Status = *update.Status
	}
	item.MarkUpdated(iamActor(r))
	if err := h.repo.SaveGroup(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMResult(w, item, nil)
}

func (h *IAMHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetGroup(r.Context(), r.PathValue("id"))
	if err != nil || item.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	writeIAMNoContent(w, h.repo.DeleteGroup(r.Context(), item.ID))
}

func (h *IAMHandler) ListGroupMembers(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListGroupMembers(r.Context(), r.PathValue("namespace"), r.PathValue("id"))
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	group, err := h.repo.GetGroup(r.Context(), r.PathValue("id"))
	if err != nil || group.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	var input struct {
		PrincipalID string `json:"principal_id"`
	}
	if !decodeIAMBody(w, r, &input) {
		return
	}
	if !h.principalBelongsToNamespace(r, input.PrincipalID) {
		http.Error(w, "principal is not a namespace member", http.StatusBadRequest)
		return
	}
	item := &entities.IAMGroupMember{
		BaseEntity: entities.BaseEntity{ID: uuid.NewString(), Namespace: group.Namespace},
		GroupID:    group.ID, PrincipalID: input.PrincipalID,
	}
	item.MarkCreated(iamActor(r))
	if err := h.repo.SaveGroupMember(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMCreated(w, item)
}

func (h *IAMHandler) DeleteGroupMember(w http.ResponseWriter, r *http.Request) {
	members, err := h.repo.ListGroupMembers(r.Context(), r.PathValue("namespace"), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	memberID := r.PathValue("member_id")
	if !slices.ContainsFunc(members, func(item *entities.IAMGroupMember) bool { return item.ID == memberID }) {
		http.NotFound(w, r)
		return
	}
	writeIAMNoContent(w, h.repo.DeleteGroupMember(r.Context(), memberID))
}

func (h *IAMHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListRoles(r.Context(), r.PathValue("namespace"))
	if err == nil {
		items = slices.DeleteFunc(items, func(item *entities.IAMRole) bool { return strings.HasPrefix(item.Name, "system-") })
	}
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) GetRole(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetRole(r.Context(), r.PathValue("id"))
	if err == nil && item.Namespace != "*" && item.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	writeIAMResult(w, item, err)
}

func (h *IAMHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var item entities.IAMRole
	if !decodeIAMBody(w, r, &item) {
		return
	}
	if strings.TrimSpace(item.Name) == "" || !validCustomPermissions(item.Permissions) {
		http.Error(w, "name and concrete catalog permissions are required", http.StatusBadRequest)
		return
	}
	item.ID = coalesceID(item.ID)
	item.Namespace = r.PathValue("namespace")
	item.Builtin = false
	item.Permissions = uniqueIAMStrings(item.Permissions)
	item.MarkCreated(iamActor(r))
	if err := h.repo.SaveRole(r.Context(), &item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMCreated(w, &item)
}

func (h *IAMHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetRole(r.Context(), r.PathValue("id"))
	if err != nil || item.Builtin || item.Namespace != r.PathValue("namespace") {
		http.Error(w, "built-in or foreign roles are immutable", http.StatusConflict)
		return
	}
	var update struct {
		Name        *string   `json:"name"`
		Description *string   `json:"description"`
		Permissions *[]string `json:"permissions"`
		Status      *string   `json:"status"`
	}
	if !decodeIAMBody(w, r, &update) {
		return
	}
	if update.Permissions != nil && !validCustomPermissions(*update.Permissions) {
		http.Error(w, "permissions must be concrete catalog entries", http.StatusBadRequest)
		return
	}
	if update.Name != nil {
		item.Name = *update.Name
	}
	if update.Description != nil {
		item.Description = *update.Description
	}
	if update.Permissions != nil {
		item.Permissions = uniqueIAMStrings(*update.Permissions)
	}
	if update.Status != nil {
		item.Status = *update.Status
	}
	item.MarkUpdated(iamActor(r))
	if err := h.repo.SaveRole(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMResult(w, item, nil)
}

func (h *IAMHandler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetRole(r.Context(), r.PathValue("id"))
	if err != nil || item.Builtin || item.Namespace != r.PathValue("namespace") {
		http.Error(w, "built-in or foreign roles are immutable", http.StatusConflict)
		return
	}
	writeIAMNoContent(w, h.repo.DeleteRole(r.Context(), item.ID))
}

func (h *IAMHandler) ListBindings(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListBindings(r.Context(), r.PathValue("namespace"))
	writeIAMResult(w, items, err)
}

// ListFlowBindings is the repository-Settings view of access grants. It is
// authorized against the Flow itself, so a delegated Flow owner does not need
// namespace-wide IAM administration privileges.
func (h *IAMHandler) ListFlowBindings(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListBindings(r.Context(), r.PathValue("namespace"))
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	flowID := r.PathValue("flow_id")
	filtered := make([]*entities.IAMRoleBinding, 0)
	for _, item := range items {
		if item.ResourceType == "flow" && item.ResourceID == flowID {
			filtered = append(filtered, item)
		}
	}
	writeIAMResult(w, filtered, nil)
}

func (h *IAMHandler) FlowAccessOptions(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	principals, err := h.repo.ListNamespacePrincipals(r.Context(), namespace)
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	groups, err := h.repo.ListGroups(r.Context(), namespace)
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	roles, err := h.repo.ListRoles(r.Context(), namespace)
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	flowRoles := make([]*entities.IAMRole, 0, len(roles))
	for _, role := range roles {
		if validFlowAccessRole(role) {
			flowRoles = append(flowRoles, role)
		}
	}
	writeIAMResult(w, map[string]any{"principals": principals, "groups": groups, "roles": flowRoles}, nil)
}

func (h *IAMHandler) CreateBinding(w http.ResponseWriter, r *http.Request) {
	h.createBinding(w, r, "", "")
}

func (h *IAMHandler) CreateFlowBinding(w http.ResponseWriter, r *http.Request) {
	h.createBinding(w, r, "flow", r.PathValue("flow_id"))
}

func (h *IAMHandler) createBinding(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	var item entities.IAMRoleBinding
	if !decodeIAMBody(w, r, &item) {
		return
	}
	if resourceType != "" {
		item.ResourceType = resourceType
		item.ResourceID = resourceID
	}
	role, err := h.repo.GetRole(r.Context(), item.RoleID)
	if err != nil || role.Status != "ACTIVE" || (role.Namespace != "*" && role.Namespace != r.PathValue("namespace")) {
		http.Error(w, "role is not available in this namespace", http.StatusBadRequest)
		return
	}
	if resourceType == "flow" && !validFlowAccessRole(role) {
		http.Error(w, "role cannot be granted at Flow scope", http.StatusBadRequest)
		return
	}
	if !validBinding(&item, r.PathValue("namespace")) {
		http.Error(w, "invalid binding subject, scope, effect, or expiration", http.StatusBadRequest)
		return
	}
	if item.SubjectType == entities.PrincipalUser || item.SubjectType == entities.PrincipalServiceAccount {
		principal, principalErr := h.repo.GetPrincipal(r.Context(), item.SubjectID)
		if principalErr != nil || principal == nil {
			http.Error(w, "binding principal does not exist", http.StatusBadRequest)
			return
		}
		if principal.Type != item.SubjectType {
			http.Error(w, "binding principal has a different type", http.StatusBadRequest)
			return
		}
		if principal.Status != "ACTIVE" || principal.DelFlag {
			http.Error(w, "binding principal is inactive", http.StatusBadRequest)
			return
		}
		if !h.principalBelongsToNamespace(r, principal.ID) {
			http.Error(w, "binding principal is not a namespace member", http.StatusBadRequest)
			return
		}
	} else if item.SubjectType == entities.PrincipalGroup {
		group, groupErr := h.repo.GetGroup(r.Context(), item.SubjectID)
		if groupErr != nil || group.Status != "ACTIVE" || group.DelFlag || group.Namespace != r.PathValue("namespace") {
			http.Error(w, "binding group does not exist in this namespace", http.StatusBadRequest)
			return
		}
	}
	item.ID = coalesceID(item.ID)
	item.Namespace = r.PathValue("namespace")
	item.Effect = strings.ToUpper(item.Effect)
	item.MarkCreated(iamActor(r))
	if err := h.repo.SaveBinding(r.Context(), &item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMCreated(w, &item)
}

func (h *IAMHandler) DeleteBinding(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetBinding(r.Context(), r.PathValue("id"))
	if err != nil || item.Namespace != r.PathValue("namespace") {
		http.NotFound(w, r)
		return
	}
	if item.RoleID == "builtin-namespace-owner" && strings.EqualFold(item.Effect, "ALLOW") {
		if h.isLastNamespaceOwner(r.Context(), item.Namespace, item.SubjectID, item.ID) {
			http.Error(w, "the last namespace owner binding cannot be removed", http.StatusConflict)
			return
		}
	}
	writeIAMNoContent(w, h.repo.DeleteBinding(r.Context(), item.ID))
}

func (h *IAMHandler) DeleteFlowBinding(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetBinding(r.Context(), r.PathValue("binding_id"))
	if err != nil || item.Namespace != r.PathValue("namespace") || item.ResourceType != "flow" || item.ResourceID != r.PathValue("flow_id") {
		http.NotFound(w, r)
		return
	}
	writeIAMNoContent(w, h.repo.DeleteBinding(r.Context(), item.ID))
}

func (h *IAMHandler) ListPermissions(w http.ResponseWriter, _ *http.Request) {
	writeIAMResult(w, authz.PermissionCatalog, nil)
}

func (h *IAMHandler) CurrentPrincipal(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	writeIAMResult(w, map[string]any{"principal": user, "namespace_id": r.PathValue("namespace")}, nil)
}

func (h *IAMHandler) Namespace(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "authenticated principal required", http.StatusUnauthorized)
		return
	}
	items, err := h.repo.ListNamespaces(r.Context())
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	visible := make([]*entities.IAMNamespace, 0, len(items))
	for _, item := range items {
		decision, decisionErr := h.authorizer.Authorize(r.Context(), authz.AccessRequest{
			PrincipalID: user.UserID, PrincipalType: user.Type, ExternalGroups: user.Groups,
			DirectPermissions: user.DirectPermissions, AllowedNamespaces: user.AllowedNamespaces,
			Namespace: item.ID, Permission: "namespace.read", ResourceType: "namespace", ResourceID: item.ID,
		})
		if decisionErr != nil {
			http.Error(w, "authorization service unavailable", http.StatusServiceUnavailable)
			return
		}
		if decision.Allowed {
			visible = append(visible, item)
		}
	}
	writeIAMResult(w, visible, nil)
}

func (h *IAMHandler) GetNamespace(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetNamespace(r.Context(), r.PathValue("id"))
	if err != nil || !h.canReadNamespace(r, item.ID) {
		http.NotFound(w, r)
		return
	}
	writeIAMResult(w, item, nil)
}

func (h *IAMHandler) CreateNamespace(w http.ResponseWriter, r *http.Request) {
	var item entities.IAMNamespace
	if !decodeIAMBody(w, r, &item) {
		return
	}
	item.ID = strings.TrimSpace(item.ID)
	if err := resourceid.ValidateNamespace(item.ID); err != nil {
		http.Error(w, "invalid namespace name: "+err.Error(), http.StatusBadRequest)
		return
	}
	if item.Name == "" {
		item.Name = item.ID
	}
	item.Name = strings.TrimSpace(item.Name)
	if err := resourceid.ValidateNamespace(item.Name); err != nil {
		http.Error(w, "invalid namespace name: "+err.Error(), http.StatusBadRequest)
		return
	}
	item.Namespace = item.ID
	item.MarkCreated(iamActor(r))
	if err := h.repo.SaveNamespace(r.Context(), &item); err != nil {
		http.Error(w, "namespace already exists or is invalid", http.StatusConflict)
		return
	}
	member := &entities.IAMNamespaceMember{
		BaseEntity:  entities.BaseEntity{ID: uuid.NewString(), Namespace: item.ID},
		PrincipalID: iamActor(r), Membership: "MEMBER",
	}
	member.MarkCreated(iamActor(r))
	if err := h.repo.SaveNamespaceMember(r.Context(), member); err != nil {
		_ = h.repo.DeleteNamespace(r.Context(), item.ID)
		http.Error(w, "namespace owner membership could not be created", http.StatusInternalServerError)
		return
	}
	ownerType := entities.PrincipalUser
	if user, ok := auth.UserFromContext(r.Context()); ok {
		ownerType = user.Type
	}
	owner := &entities.IAMRoleBinding{
		BaseEntity: entities.BaseEntity{ID: uuid.NewString(), Namespace: item.ID},
		RoleID:     "builtin-namespace-owner", SubjectType: ownerType, SubjectID: iamActor(r),
		ResourceType: "namespace", ResourceID: "*", Effect: "ALLOW",
	}
	owner.MarkCreated(iamActor(r))
	if err := h.repo.SaveBinding(r.Context(), owner); err != nil {
		_ = h.repo.RemoveNamespaceMember(r.Context(), item.ID, iamActor(r))
		_ = h.repo.DeleteNamespace(r.Context(), item.ID)
		http.Error(w, "namespace owner binding could not be created", http.StatusInternalServerError)
		return
	}
	writeIAMCreated(w, &item)
}

func (h *IAMHandler) UpdateNamespace(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetNamespace(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var update struct {
		Name        *string            `json:"name"`
		Description *string            `json:"description"`
		Labels      *map[string]string `json:"labels"`
		Status      *string            `json:"status"`
	}
	if !decodeIAMBody(w, r, &update) {
		return
	}
	if update.Name != nil {
		name := strings.TrimSpace(*update.Name)
		if err := resourceid.ValidateNamespace(name); err != nil {
			http.Error(w, "invalid namespace name: "+err.Error(), http.StatusBadRequest)
			return
		}
		item.Name = name
	}
	if update.Description != nil {
		item.Description = *update.Description
	}
	if update.Labels != nil {
		item.Labels = *update.Labels
	}
	if update.Status != nil {
		item.Status = *update.Status
	}
	item.MarkUpdated(iamActor(r))
	if err := h.repo.SaveNamespace(r.Context(), item); err != nil {
		http.Error(w, "namespace update conflict", http.StatusConflict)
		return
	}
	writeIAMResult(w, item, nil)
}

func (h *IAMHandler) DeleteNamespace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "default" {
		http.Error(w, "the default namespace cannot be deleted", http.StatusConflict)
		return
	}
	if _, err := h.repo.GetNamespace(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	bindings, err := h.repo.ListBindings(r.Context(), id)
	if err != nil {
		writeIAMResult(w, nil, err)
		return
	}
	if len(bindings) > 0 {
		http.Error(w, "remove namespace role bindings before deletion", http.StatusConflict)
		return
	}
	writeIAMNoContent(w, h.repo.DeleteNamespace(r.Context(), id))
}

func (h *IAMHandler) canReadNamespace(r *http.Request, namespace string) bool {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		return false
	}
	decision, err := h.authorizer.Authorize(r.Context(), authz.AccessRequest{
		PrincipalID: user.UserID, PrincipalType: user.Type, ExternalGroups: user.Groups,
		DirectPermissions: user.DirectPermissions, AllowedNamespaces: user.AllowedNamespaces,
		Namespace: namespace, Permission: "namespace.read", ResourceType: "namespace", ResourceID: namespace,
	})
	return err == nil && decision.Allowed
}

func (h *IAMHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListAPIKeys(r.Context(), r.PathValue("namespace"))
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "authenticated principal required", http.StatusUnauthorized)
		return
	}
	var input struct {
		Name        string     `json:"name"`
		PrincipalID string     `json:"principal_id"`
		Permissions []string   `json:"permissions"`
		ExpiresAt   *time.Time `json:"expires_at"`
	}
	if !decodeIAMBody(w, r, &input) {
		return
	}
	if input.Name == "" || !validCustomPermissions(input.Permissions) {
		http.Error(w, "name and concrete permissions are required", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	if input.ExpiresAt == nil {
		defaultExpiry := now.Add(90 * 24 * time.Hour)
		input.ExpiresAt = &defaultExpiry
	}
	if !input.ExpiresAt.After(now) || input.ExpiresAt.After(now.Add(366*24*time.Hour)) {
		http.Error(w, "API key expiration must be within 366 days", http.StatusBadRequest)
		return
	}
	namespace := r.PathValue("namespace")
	targetID := strings.TrimSpace(input.PrincipalID)
	if targetID == "" {
		targetID = user.UserID
	}
	target, err := h.repo.GetPrincipal(r.Context(), targetID)
	if err != nil || target == nil || target.Status != "ACTIVE" || target.DelFlag || !h.principalBelongsToNamespace(r, targetID) {
		http.Error(w, "API key principal is not an active namespace member", http.StatusBadRequest)
		return
	}
	if targetID != user.UserID && target.Type != entities.PrincipalServiceAccount {
		http.Error(w, "administrators may issue API keys only for service accounts", http.StatusBadRequest)
		return
	}
	for _, permission := range input.Permissions {
		decision, err := h.authorizer.Authorize(r.Context(), authz.AccessRequest{
			PrincipalID: user.UserID, PrincipalType: user.Type, ExternalGroups: user.Groups,
			DirectPermissions: user.DirectPermissions, AllowedNamespaces: user.AllowedNamespaces,
			Namespace: namespace, Permission: permission, ResourceType: "namespace", ResourceID: namespace,
		})
		if err != nil || !decision.Allowed {
			http.Error(w, "API key permissions may not exceed the caller", http.StatusForbidden)
			return
		}
	}
	id, raw, hash, prefix, suffix, err := authz.GenerateAPIKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item := &entities.IAMAPIKey{
		BaseEntity: entities.BaseEntity{ID: id}, Name: input.Name, PrincipalID: targetID,
		SecretHash: hash, Prefix: prefix, Suffix: suffix,
		AllowedNamespaces: []string{namespace}, Permissions: uniqueIAMStrings(input.Permissions), ExpiresAt: input.ExpiresAt,
	}
	item.MarkCreated(user.UserID)
	if err := h.repo.SaveAPIKey(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeIAMCreated(w, map[string]any{"record": item, "secret": raw})
}

func (h *IAMHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	item, err := h.repo.GetAPIKey(r.Context(), r.PathValue("id"))
	if err != nil || !slices.Contains(item.AllowedNamespaces, r.PathValue("namespace")) {
		http.NotFound(w, r)
		return
	}
	now := time.Now().UTC()
	item.Status = "REVOKED"
	item.RevokedAt = &now
	item.MarkUpdated(iamActor(r))
	writeIAMNoContent(w, h.repo.SaveAPIKey(r.Context(), item))
}

func (h *IAMHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListAudit(r.Context(), r.PathValue("namespace"), 500)
	writeIAMResult(w, items, err)
}

func (h *IAMHandler) principalBelongsToNamespace(r *http.Request, id string) bool {
	member, err := h.repo.GetNamespaceMember(r.Context(), r.PathValue("namespace"), id)
	return err == nil && member != nil && member.Status == "ACTIVE" && !member.DelFlag
}

func (h *IAMHandler) isLastNamespaceOwner(ctx context.Context, namespace, removingPrincipal, removingBinding string) bool {
	bindings, err := h.repo.ListBindings(ctx, namespace)
	if err != nil {
		return true
	}
	now := time.Now().UTC()
	remaining := 0
	for _, binding := range bindings {
		if binding.ID == removingBinding || (removingBinding == "" && binding.SubjectID == removingPrincipal) {
			continue
		}
		if binding.RoleID == "builtin-namespace-owner" && binding.ResourceType == "namespace" &&
			(binding.ResourceID == "*" || binding.ResourceID == namespace) && strings.EqualFold(binding.Effect, "ALLOW") &&
			binding.Status == "ACTIVE" && !binding.DelFlag && (binding.ExpiresAt == nil || binding.ExpiresAt.After(now)) {
			remaining++
		}
	}
	return remaining == 0
}

func validFlowAccessRole(role *entities.IAMRole) bool {
	if role == nil || role.Status != "ACTIVE" || strings.HasPrefix(role.ID, "builtin-system-") ||
		role.ID == "builtin-platform-owner" || role.ID == "builtin-namespace-owner" || role.ID == "builtin-secret-manager" {
		return false
	}
	return slices.ContainsFunc(role.Permissions, func(permission string) bool {
		return permission == "*" || strings.HasPrefix(permission, "flow.") || strings.HasPrefix(permission, "run.") ||
			strings.HasPrefix(permission, "task.") || strings.HasPrefix(permission, "approval.") || strings.HasPrefix(permission, "trace.")
	})
}

func decodeIAMBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxIAMBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "request body must contain exactly one JSON value", http.StatusBadRequest)
		return false
	}
	return true
}

func writeIAMCreated(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(value)
}

func writeIAMResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		if isNotFoundError(err) {
			http.Error(w, "not found", http.StatusNotFound)
		} else {
			slog.Error("iam request failed", "error", err)
			http.Error(w, "IAM service unavailable", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeIAMNoContent(w http.ResponseWriter, err error) {
	if err != nil {
		slog.Error("iam mutation failed", "error", err)
		http.Error(w, "IAM service unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func iamActor(r *http.Request) string {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		return user.UserID
	}
	return ""
}

func coalesceID(value string) string {
	if value != "" {
		return value
	}
	return uuid.NewString()
}

func validCustomPermissions(permissions []string) bool {
	return len(permissions) > 0 && slices.ContainsFunc(permissions, func(permission string) bool {
		return !slices.Contains(authz.PermissionCatalog, permission)
	}) == false
}

func validBinding(item *entities.IAMRoleBinding, namespace string) bool {
	if item.SubjectID == "" || (item.SubjectType != entities.PrincipalUser && item.SubjectType != entities.PrincipalGroup && item.SubjectType != entities.PrincipalServiceAccount) {
		return false
	}
	if item.ResourceID == "" || !slices.Contains([]string{"namespace", "agent", "skill", "flow", "flow_release", "llm_provider", "mcp", "notification", "knowledge", "run", "task", "approval", "trace", "service_account"}, item.ResourceType) {
		return false
	}
	if item.ResourceType == "namespace" && item.ResourceID != "*" && item.ResourceID != namespace {
		return false
	}
	for key, raw := range item.Conditions {
		if key != "source_cidrs" {
			return false
		}
		values, ok := raw.([]any)
		if !ok || len(values) == 0 {
			return false
		}
		for _, value := range values {
			cidr, ok := value.(string)
			if !ok {
				return false
			}
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return false
			}
		}
	}
	effect := strings.ToUpper(item.Effect)
	if effect == "" {
		item.Effect = "ALLOW"
		effect = "ALLOW"
	}
	return (effect == "ALLOW" || effect == "DENY") && (item.ExpiresAt == nil || item.ExpiresAt.After(time.Now()))
}

func uniqueIAMStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	slices.Sort(result)
	return result
}
