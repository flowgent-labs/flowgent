package storage

import (
	"context"
	"errors"
	"strconv"
	"strings"

	guardmodel "authguard/adapters/golang/model"
)

var ErrFlowgentSqlScopeDenied = errors.New("row is outside the Flowgent authorization scope")

// FlowgentSqlScope embeds the official AuthGuard SDK scope while keeping SDK
// construction and context propagation out of individual repositories. An
// absent scope deliberately resolves to allow-all so local development and
// deployments with AuthGuard integration disabled remain fully operational.
type FlowgentSqlScope struct {
	guardmodel.SqlScope
}

type flowgentSqlScopeContextKey struct{}
type flowgentSqlScopeResolverContextKey struct{}

type flowgentSqlScopeResolverContext struct {
	requestPath []string
	resolver    FlowgentSqlScopeResolver
}

// FlowgentResourcePath is the part of an AuthGuard resource mapping below
// namespace/{namespace_id}. Repository packages build paths through the
// helpers below without importing the SDK directly.
type FlowgentResourcePath []guardmodel.PathMap

type FlowgentSqlScopeResolver func(FlowgentResourcePath) (guardmodel.SqlScope, error)

const flowNameForRunResource = `(SELECT f.name FROM orh_run r JOIN orh_flow f ON f.id = r.flow_id WHERE r.id = "run_id")`
const currentFlowNameResource = `(SELECT name FROM orh_flow WHERE id = "flow_id")`

// resourcePathsByTable is the single storage-owned registry that maps
// persisted rows to AuthGuard resource paths. Entity repositories identify
// their table; they do not carry duplicated authorization wiring.
var resourcePathsByTable = map[string][]FlowgentResourcePath{
	"llm_agent": {
		ResourcePath(LiteralResource("agents"), ColumnResource(`"name"`)),
	},
	"orh_approval": {
		ResourcePath(LiteralResource("approvals"), ColumnResource(`"id"`)),
		ResourcePath(
			LiteralResource("runs"),
			ColumnResource(`"run_id"`),
			LiteralResource("approvals"),
			ColumnResource(`"id"`),
		),
		ResourcePath(
			LiteralResource("flows"),
			ColumnResource(flowNameForRunResource),
			LiteralResource("runs"),
			ColumnResource(`"run_id"`),
			LiteralResource("approvals"),
			ColumnResource(`"id"`),
		),
	},
	"orh_flow": {
		ResourcePath(LiteralResource("flows"), ColumnResource(`"name"`)),
		ResourcePath(LiteralResource("skills"), ColumnResource(`"name"`)),
		ResourcePath(LiteralResource("flows"), LiteralResource("watch"), ColumnResource(`"name"`)),
		ResourcePath(LiteralResource("flows"), LiteralResource("trigger"), ColumnResource(`"name"`)),
		ResourcePath(LiteralResource("flows"), ColumnResource(`"name"`), LiteralResource("trigger")),
	},
	"orh_flow_release": {
		ResourcePath(LiteralResource("flow-releases"), ColumnResource(`"id"`)),
	},
	"orh_flow_release_grant": {
		ResourcePath(
			LiteralResource("flow-releases"),
			ColumnResource(`"release_id"`),
			LiteralResource("grants"),
			ColumnResource(`"id"`),
		),
	},
	"orh_flow_installation": {
		ResourcePath(LiteralResource("flow-installations"), ColumnResource(`"id"`)),
		ResourcePath(
			LiteralResource("flow-releases"),
			ColumnResource(`"release_id"`),
			LiteralResource("install"),
		),
	},
	"orh_run": {
		ResourcePath(LiteralResource("runs"), ColumnResource(`"id"`)),
		ResourcePath(
			LiteralResource("flows"),
			ColumnResource(currentFlowNameResource),
			LiteralResource("runs"),
			ColumnResource(`"id"`),
		),
		ResourcePath(LiteralResource("flows"), ColumnResource(currentFlowNameResource), LiteralResource("trigger")),
		ResourcePath(LiteralResource("flows"), LiteralResource("trigger"), ColumnResource(currentFlowNameResource)),
		ResourcePath(LiteralResource("runs"), ColumnResource(`"id"`), LiteralResource("cancel")),
		ResourcePath(LiteralResource("runs"), LiteralResource("metrics"), ColumnResource(`"id"`)),
	},
	"knw_document": {
		ResourcePath(LiteralResource("knowledge"), ColumnResource(`"id"`)),
		ResourcePath(LiteralResource("knowledge"), LiteralResource("tags"), ColumnResource(`"id"`)),
		ResourcePath(LiteralResource("knowledge"), LiteralResource("search"), ColumnResource(`"id"`)),
	},
	"llm_provider": {
		ResourcePath(LiteralResource("llm"), LiteralResource("providers"), ColumnResource(`"id"`)),
	},
	"llm_mcp": {
		ResourcePath(LiteralResource("mcp"), ColumnResource(`"name"`)),
	},
	"nfy_channel": {
		ResourcePath(LiteralResource("notifications"), LiteralResource("channels"), ColumnResource(`"id"`)),
		ResourcePath(LiteralResource("notifications"), LiteralResource("runtime"), LiteralResource("channels"), ColumnResource(`"id"`)),
		ResourcePath(LiteralResource("notifications"), LiteralResource("test"), ColumnResource(`"id"`)),
	},
	"orh_runtime_configuration": runtimeConfigurationResourcePaths(),
	"llm_skill": {
		ResourcePath(LiteralResource("skill-definitions"), ColumnResource(`"name"`)),
	},
	"orh_node_run": {
		ResourcePath(
			LiteralResource("runs"),
			ColumnResource(`"run_id"`),
			LiteralResource("node-runs"),
			ColumnResource(`"id"`),
		),
		ResourcePath(
			LiteralResource("flows"),
			ColumnResource(flowNameForRunResource),
			LiteralResource("runs"),
			ColumnResource(`"run_id"`),
			LiteralResource("node-runs"),
			ColumnResource(`"id"`),
		),
	},
}

func runtimeConfigurationResourcePaths() []FlowgentResourcePath {
	paths := []FlowgentResourcePath{
		ResourcePath(LiteralResource("runtime-config")),
		ResourcePath(LiteralResource("runtime-config"), LiteralResource("environment")),
		ResourcePath(LiteralResource("runtime-config"), LiteralResource("secrets")),
	}
	for _, suffix := range []string{"", "environment", "secrets", "resolved"} {
		path := ResourcePath(
			LiteralResource("flows"),
			ColumnResource(currentFlowNameResource),
			LiteralResource("runtime-config"),
		)
		if suffix != "" {
			path = append(path, LiteralResource(suffix))
		}
		paths = append(paths, path)
	}
	return paths
}

func ResourcePath(segments ...guardmodel.PathMap) FlowgentResourcePath {
	return append(FlowgentResourcePath(nil), segments...)
}

func LiteralResource(value string) guardmodel.PathMap {
	return guardmodel.LiteralPath(value)
}

func ColumnResource(column string) guardmodel.PathMap {
	return guardmodel.ColumnPath(column)
}

// NewFlowgentSqlScope wraps an SDK scope and owns a defensive copy of its
// parameters so request-scoped authorization data cannot be mutated by a
// repository.
func NewFlowgentSqlScope(scope guardmodel.SqlScope) FlowgentSqlScope {
	return FlowgentSqlScope{SqlScope: guardmodel.SqlScope{
		Where: scope.Where,
		Args:  append([]any(nil), scope.Args...),
	}}
}

func DummyFlowgentSqlScope() FlowgentSqlScope {
	return NewFlowgentSqlScope(guardmodel.SqlScope{Where: "1=1", Args: []any{}})
}

func DenyFlowgentSqlScope() FlowgentSqlScope {
	return NewFlowgentSqlScope(guardmodel.SqlScope{Where: "0=1", Args: []any{}})
}

// NamespaceFlowgentSqlScope is the trusted Flowgent adapter for AuthGuard URNs
// whose first resource path segment is namespace/{namespace_id}.
func NamespaceFlowgentSqlScope(namespace string) FlowgentSqlScope {
	if strings.TrimSpace(namespace) == "" {
		return DenyFlowgentSqlScope()
	}
	return NewFlowgentSqlScope(guardmodel.SqlScope{Where: `"namespace_id" = ?`, Args: []any{namespace}})
}

func WithFlowgentSqlScope(ctx context.Context, scope FlowgentSqlScope) context.Context {
	return context.WithValue(ctx, flowgentSqlScopeContextKey{}, NewFlowgentSqlScope(scope.SqlScope))
}

func WithFlowgentSqlScopeResolver(
	ctx context.Context,
	requestPath []string,
	resolver FlowgentSqlScopeResolver,
) context.Context {
	value := flowgentSqlScopeResolverContext{
		requestPath: append([]string(nil), requestPath...),
		resolver:    resolver,
	}
	return context.WithValue(ctx, flowgentSqlScopeResolverContextKey{}, value)
}

func FlowgentSqlScopeFromContext(ctx context.Context) FlowgentSqlScope {
	if scope, ok := ctx.Value(flowgentSqlScopeContextKey{}).(FlowgentSqlScope); ok && scope.Where != "" {
		return NewFlowgentSqlScope(scope.SqlScope)
	}
	return DummyFlowgentSqlScope()
}

// FlowgentSqlScopeForTable resolves a table through the centralized resource
// registry. An unknown table fails closed whenever AuthGuard supplied a scope
// resolver, while standalone and trusted-control-plane contexts remain dummy.
func FlowgentSqlScopeForTable(ctx context.Context, table string) FlowgentSqlScope {
	paths, registered := resourcePathsByTable[table]
	if !registered {
		if resolverContext, ok := ctx.Value(flowgentSqlScopeResolverContextKey{}).(flowgentSqlScopeResolverContext); ok && resolverContext.resolver != nil {
			return DenyFlowgentSqlScope()
		}
		return FlowgentSqlScopeFromContext(ctx)
	}
	return FlowgentSqlScopeForResources(ctx, paths...)
}

// FlowgentSqlScopeForResources compiles the current AuthGuard grant set against
// one or more repository row mappings. Multiple API aliases are combined with
// OR. Missing resolvers (AuthGuard disabled or trusted control plane) retain
// the context's dummy scope; resolver failures fail closed.
func FlowgentSqlScopeForResources(ctx context.Context, paths ...FlowgentResourcePath) FlowgentSqlScope {
	resolverContext, ok := ctx.Value(flowgentSqlScopeResolverContextKey{}).(flowgentSqlScopeResolverContext)
	if !ok || resolverContext.resolver == nil || len(paths) == 0 {
		return FlowgentSqlScopeFromContext(ctx)
	}
	paths = bestResourcePaths(resolverContext.requestPath, paths)
	if len(paths) == 0 {
		return DenyFlowgentSqlScope()
	}
	scopes := make([]FlowgentSqlScope, 0, len(paths))
	for _, path := range paths {
		scope, err := resolverContext.resolver(append(FlowgentResourcePath(nil), path...))
		if err != nil {
			return DenyFlowgentSqlScope()
		}
		wrapped := NewFlowgentSqlScope(scope)
		if wrapped.Where == "1=1" {
			return wrapped
		}
		if wrapped.Where != "0=1" {
			scopes = append(scopes, wrapped)
		}
	}
	if len(scopes) == 0 {
		return DenyFlowgentSqlScope()
	}
	if len(scopes) == 1 {
		return scopes[0]
	}
	parts := make([]string, 0, len(scopes))
	args := make([]any, 0)
	for _, scope := range scopes {
		parts = append(parts, "("+scope.Where+")")
		args = append(args, scope.Args...)
	}
	return NewFlowgentSqlScope(guardmodel.SqlScope{Where: strings.Join(parts, " OR "), Args: args})
}

func bestResourcePaths(requestPath []string, paths []FlowgentResourcePath) []FlowgentResourcePath {
	bestLiteralMatches, bestDistance := -1, int(^uint(0)>>1)
	selected := make([]FlowgentResourcePath, 0, len(paths))
	for _, path := range paths {
		literalMatches, compatible := resourcePathScore(requestPath, path)
		if !compatible {
			continue
		}
		distance := len(path) - len(requestPath)
		if distance < 0 {
			distance = -distance
		}
		if literalMatches > bestLiteralMatches || literalMatches == bestLiteralMatches && distance < bestDistance {
			bestLiteralMatches, bestDistance = literalMatches, distance
			selected = selected[:0]
		}
		if literalMatches == bestLiteralMatches && distance == bestDistance {
			selected = append(selected, path)
		}
	}
	return selected
}

func resourcePathScore(requestPath []string, path FlowgentResourcePath) (int, bool) {
	overlap := len(requestPath)
	if len(path) < overlap {
		overlap = len(path)
	}
	literalMatches := 0
	for index := 0; index < overlap; index++ {
		segment := path[index]
		switch segment.Kind {
		case guardmodel.PathLiteral:
			if segment.Value != requestPath[index] {
				return 0, false
			}
			literalMatches++
		case guardmodel.PathColumn:
			// A column consumes exactly one concrete request segment.
		case guardmodel.PathRemainderColumn:
			return literalMatches, true
		default:
			return 0, false
		}
	}
	return literalMatches, true
}

// AuthGuardSqlScope unwraps a defensive SDK value for integrations that need
// to pass the scope to an AuthGuard-aware repository API.
func (s FlowgentSqlScope) AuthGuardSqlScope() guardmodel.SqlScope {
	return NewFlowgentSqlScope(s.SqlScope).SqlScope
}

func (s FlowgentSqlScope) sqliteWhere() (string, []any) {
	return s.SQLiteWhere()
}

// SQLiteWhere returns a defensive copy of the SDK-compatible predicate and
// arguments for repositories with custom SQLite queries.
func (s FlowgentSqlScope) SQLiteWhere() (string, []any) {
	if s.Where == "" {
		s = DummyFlowgentSqlScope()
	}
	return s.Where, append([]any(nil), s.Args...)
}

func (s FlowgentSqlScope) postgresWhere(firstParameter int) (string, []any) {
	return s.PostgresWhere(firstParameter)
}

// PostgresWhere rebinds the SDK-compatible question-mark placeholders to
// PostgreSQL parameters, starting at firstParameter.
func (s FlowgentSqlScope) PostgresWhere(firstParameter int) (string, []any) {
	where, args := s.SQLiteWhere()
	for index := range args {
		where = strings.Replace(where, "?", "$"+strconv.Itoa(firstParameter+index), 1)
	}
	return where, args
}
