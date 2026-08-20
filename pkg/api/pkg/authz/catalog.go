package authz

// PermissionCatalog is the authoritative UI/API permission vocabulary. Wildcard
// permissions may be used in roles, but API keys must use concrete entries.
var PermissionCatalog = []string{
	"platform.namespace.manage",
	"namespace.read", "namespace.runtime.manage", "namespace.config.read", "namespace.config.manage", "namespace.secret.manage",
	"agent.read", "agent.use", "agent.write", "agent.delete",
	"skill.read", "skill.use", "skill.write", "skill.delete",
	"flow.read", "flow.use", "flow.write", "flow.delete", "flow.access.manage", "flow.config.read", "flow.config.manage", "flow.secret.manage", "flow.runtime.use",
	"flow_release.read", "flow_release.publish", "flow_release.install", "flow_release.revoke",
	"llm_provider.read", "llm_provider.use", "llm_provider.write", "llm_provider.delete", "llm_provider.secret.manage",
	"mcp.read", "mcp.use", "mcp.write", "mcp.delete", "mcp.secret.manage",
	"notification.read", "notification.write", "notification.delete", "notification.test", "notification.secret.manage", "notification.emit", "notification.internal.deliver",
	"knowledge.read", "knowledge.write", "knowledge.delete",
	"run.read", "run.trigger", "run.cancel", "run.delete", "run.internal.create", "run.internal.update",
	"task.read", "task.internal.write",
	"approval.read", "approval.resolve", "approval.internal.create", "approval.platform.read",
	"trace.read",
	"iam.principal.read", "iam.principal.write", "iam.principal.delete",
	"iam.group.read", "iam.group.write", "iam.group.delete",
	"iam.role.read", "iam.role.write", "iam.role.delete",
	"iam.binding.read", "iam.binding.write", "iam.binding.delete",
	"iam.namespace.read", "iam.namespace.write",
	"iam.audit.read", "service_account.read", "service_account.write", "service_account.revoke",
}

func permissionMatches(granted, requested string) bool {
	if granted == "*" || granted == requested {
		return true
	}
	if len(granted) > 2 && granted[len(granted)-2:] == ".*" {
		prefix := granted[:len(granted)-1]
		return len(requested) >= len(prefix) && requested[:len(prefix)] == prefix
	}
	return false
}

func anyPermissionMatches(granted []string, requested string) bool {
	for _, permission := range granted {
		if permissionMatches(permission, requested) {
			return true
		}
	}
	return false
}
