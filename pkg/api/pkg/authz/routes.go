package authz

import (
	"net/http"
	"strings"
)

type RoutePolicy struct {
	Permission   string
	Namespace    string
	ResourceType string
	ResourceID   string
}

// PolicyForRequest converts the REST surface to one stable authorization
// vocabulary. A protected but unknown API path intentionally has no policy and
// is denied by the middleware.
func PolicyForRequest(r *http.Request) (RoutePolicy, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return RoutePolicy{}, false
	}
	if parts[2] == "webhook" {
		return RoutePolicy{}, false
	}
	if len(parts) < 4 {
		return RoutePolicy{}, false
	}
	namespace := parts[2]
	root := parts[3]
	rest := parts[4:]
	policy := RoutePolicy{Namespace: namespace, ResourceType: root, ResourceID: "*"}

	switch root {
	case "agents":
		policy.ResourceType = "agent"
		policy.ResourceID = pathID(rest)
		policy.Permission = crudPermission("agent", r.Method)
	case "skills":
		policy.ResourceType = "skill"
		policy.ResourceID = pathID(rest)
		policy.Permission = crudPermission("skill", r.Method)
	case "flows":
		policy.ResourceType = "flow"
		if len(rest) >= 2 && rest[1] == "runtime-config" {
			policy.ResourceID = rest[0]
			policy.Permission = runtimeConfigPermission("flow", r.Method, rest[2:])
		} else if len(rest) >= 3 && rest[1] == "iam" {
			policy.ResourceID = rest[0]
			policy.Permission = "flow.access.manage"
		} else if len(rest) >= 2 && rest[1] == "runs" {
			policy.ResourceID = rest[0]
			policy.Permission = flowScopedRunPermission(r.Method, rest[2:])
		} else if r.Method == http.MethodPost && ((len(rest) == 1 && rest[0] == "trigger") || (len(rest) == 2 && rest[1] == "trigger")) {
			policy.Permission = "run.trigger"
			if len(rest) == 2 {
				policy.ResourceID = rest[0]
			}
		} else {
			if len(rest) > 0 && rest[0] != "watch" {
				policy.ResourceID = rest[0]
			}
			policy.Permission = crudPermission("flow", r.Method)
		}
	case "flow-releases":
		policy.ResourceType = "flow_release"
		policy.ResourceID = pathID(rest)
		policy.Permission = flowReleasePermission(r.Method, rest)
	case "flow-installations":
		policy.ResourceType = "flow_release"
		policy.ResourceID = pathID(rest)
		if r.Method == http.MethodGet {
			policy.Permission = "flow_release.read"
		}
	case "runs":
		policy.ResourceType = "run"
		if len(rest) == 1 && rest[0] == "metrics" {
			policy.ResourceID = "*"
		} else {
			policy.ResourceID = pathID(rest)
		}
		policy.Permission = runPermission(r.Method, rest)
		if len(rest) >= 2 {
			switch rest[1] {
			case "tasks":
				policy.ResourceType = "task"
				if len(rest) >= 3 {
					policy.ResourceID = rest[2]
				}
			case "approvals":
				policy.ResourceType = "approval"
			case "trace":
				policy.ResourceType = "trace"
			}
		}
	case "approvals":
		policy.ResourceType = "approval"
		policy.ResourceID = "*"
		if r.Method == http.MethodGet {
			policy.Permission = "approval.platform.read"
		}
	case "notifications":
		policy.ResourceType = "notification"
		if len(rest) == 2 && rest[0] == "runtime" && rest[1] == "channels" && r.Method == http.MethodGet {
			policy.Permission = "notification.internal.deliver"
			break
		}
		if len(rest) >= 2 && rest[0] == "channels" {
			policy.ResourceID = rest[1]
		}
		if len(rest) > 0 && rest[0] == "test" {
			policy.Permission = "notification.test"
		} else if len(rest) > 0 && rest[0] == "channels" && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			policy.Permission = "notification.secret.manage"
		} else {
			policy.Permission = crudPermission("notification", r.Method)
		}
	case "llm":
		policy.ResourceType = "llm_provider"
		if len(rest) >= 2 && rest[0] == "providers" {
			policy.ResourceID = rest[1]
		}
		policy.Permission = crudPermission("llm_provider", r.Method)
	case "mcp":
		policy.ResourceType = "mcp"
		policy.ResourceID = pathID(rest)
		policy.Permission = crudPermission("mcp", r.Method)
	case "knowledge":
		policy.ResourceType = "knowledge"
		if len(rest) > 0 && rest[0] != "search" && rest[0] != "tags" {
			policy.ResourceID = rest[0]
		}
		if r.Method == http.MethodPost && len(rest) > 0 && rest[0] == "search" {
			policy.Permission = "knowledge.read"
		} else {
			policy.Permission = crudPermission("knowledge", r.Method)
		}
	case "ws":
		if r.Method == http.MethodGet && len(rest) == 1 && rest[0] == "human-approvals" {
			policy.ResourceType = "approval"
			policy.Permission = "approval.read"
		}
	case "iam":
		return iamPolicy(r.Method, namespace, rest)
	case "runtime-config":
		policy.ResourceType = "namespace"
		policy.ResourceID = namespace
		policy.Permission = runtimeConfigPermission("namespace", r.Method, rest)
	default:
		return RoutePolicy{}, false
	}
	return policy, policy.Permission != ""
}

func runtimeConfigPermission(resource, method string, rest []string) string {
	if method == http.MethodGet {
		if len(rest) == 1 && rest[0] == "resolved" {
			return "flow.runtime.use"
		}
		return resource + ".config.read"
	}
	if method != http.MethodPut || len(rest) != 1 {
		return ""
	}
	switch rest[0] {
	case "environment":
		return resource + ".config.manage"
	case "secrets":
		return resource + ".secret.manage"
	default:
		return ""
	}
}

func flowScopedRunPermission(method string, rest []string) string {
	if len(rest) == 0 {
		if method == http.MethodGet {
			return "run.read"
		}
		return ""
	}
	if len(rest) >= 2 {
		switch rest[1] {
		case "cancel":
			return "run.cancel"
		case "trace":
			return "trace.read"
		case "tasks":
			return "task.read"
		case "approvals":
			if method == http.MethodPost && len(rest) == 2 {
				return "approval.internal.create"
			}
			if method == http.MethodGet {
				return "approval.read"
			}
			return "approval.resolve"
		}
	}
	return crudPermission("run", method)
}

func flowReleasePermission(method string, rest []string) string {
	if len(rest) == 0 {
		if method == http.MethodPost {
			return "flow_release.publish"
		}
		if method == http.MethodGet {
			return "flow_release.read"
		}
		return ""
	}
	if len(rest) >= 2 {
		switch rest[1] {
		case "install":
			return "flow_release.install"
		case "revoke":
			return "flow_release.revoke"
		case "grants":
			if method == http.MethodGet {
				return "flow_release.read"
			}
			if method == http.MethodDelete {
				return "flow_release.revoke"
			}
			return "flow_release.publish"
		}
	}
	if method == http.MethodGet {
		return "flow_release.read"
	}
	return ""
}

func pathID(parts []string) string {
	if len(parts) == 0 {
		return "*"
	}
	return parts[0]
}

func crudPermission(resource, method string) string {
	switch method {
	case http.MethodGet:
		return resource + ".read"
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return resource + ".write"
	case http.MethodDelete:
		return resource + ".delete"
	default:
		return ""
	}
}

func runPermission(method string, rest []string) string {
	if len(rest) == 0 {
		if method == http.MethodPost {
			return "run.internal.create"
		}
		return crudPermission("run", method)
	}
	if len(rest) >= 2 {
		switch rest[1] {
		case "cancel":
			return "run.cancel"
		case "trace":
			return "trace.read"
		case "tasks":
			if method == http.MethodGet {
				return "task.read"
			}
			return "task.internal.write"
		case "approvals":
			if method == http.MethodPost && len(rest) == 2 {
				return "approval.internal.create"
			}
			if method == http.MethodGet {
				return "approval.read"
			}
			return "approval.resolve"
		}
	}
	if method == http.MethodPut || method == http.MethodPatch {
		return "run.internal.update"
	}
	return crudPermission("run", method)
}

func iamPolicy(method, namespace string, rest []string) (RoutePolicy, bool) {
	if len(rest) == 0 {
		return RoutePolicy{}, false
	}
	resource := rest[0]
	id := "*"
	if len(rest) > 1 {
		id = rest[1]
	}
	base := "iam." + strings.TrimSuffix(resource, "s")
	if resource == "permissions" {
		base = "iam.role"
	}
	if resource == "namespaces" {
		if method == http.MethodGet {
			return RoutePolicy{Namespace: namespace, ResourceType: "namespace", ResourceID: id, Permission: "namespace.read"}, true
		}
		return RoutePolicy{Namespace: "*", ResourceType: "platform", ResourceID: id, Permission: "platform.namespace.manage"}, true
	}
	if resource == "me" {
		base = "namespace"
	}
	if resource == "api-keys" {
		base = "service_account"
		resource = "service_account"
	}
	if resource == "audit" {
		base = "iam.audit"
	}
	policy := RoutePolicy{Namespace: namespace, ResourceType: resource, ResourceID: id}
	switch method {
	case http.MethodGet:
		policy.Permission = base + ".read"
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		policy.Permission = base + ".write"
	case http.MethodDelete:
		if resource == "service_account" {
			policy.Permission = "service_account.revoke"
		} else {
			policy.Permission = base + ".delete"
		}
	}
	return policy, policy.Permission != ""
}
