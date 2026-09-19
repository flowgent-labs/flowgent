package store

import (
	"context"
	"strconv"
	"strings"
)

// FlowgentSqlScope is the storage-owned boundary around an AuthGuard-compatible
// SqlScope. Keeping the wrapper here prevents repositories from importing the
// optional AuthGuard adapter SDK. An absent scope is deliberately allow-all so
// deployments with AuthGuard integration disabled retain native IAM behavior.
type FlowgentSqlScope struct {
	Where string
	Args  []any
}

type flowgentSqlScopeContextKey struct{}

func DummyFlowgentSqlScope() FlowgentSqlScope {
	return FlowgentSqlScope{Where: "1=1"}
}

func DenyFlowgentSqlScope() FlowgentSqlScope {
	return FlowgentSqlScope{Where: "0=1"}
}

// NamespaceFlowgentSqlScope is the trusted Flowgent adapter for AuthGuard URNs
// whose first resource path segment is namespace/{namespace_id}.
func NamespaceFlowgentSqlScope(namespace string) FlowgentSqlScope {
	if strings.TrimSpace(namespace) == "" {
		return DenyFlowgentSqlScope()
	}
	return FlowgentSqlScope{Where: `"namespace_id" = ?`, Args: []any{namespace}}
}

func WithFlowgentSqlScope(ctx context.Context, scope FlowgentSqlScope) context.Context {
	return context.WithValue(ctx, flowgentSqlScopeContextKey{}, scope)
}

func FlowgentSqlScopeFromContext(ctx context.Context) FlowgentSqlScope {
	if scope, ok := ctx.Value(flowgentSqlScopeContextKey{}).(FlowgentSqlScope); ok && scope.Where != "" {
		return FlowgentSqlScope{Where: scope.Where, Args: append([]any(nil), scope.Args...)}
	}
	return DummyFlowgentSqlScope()
}

func (s FlowgentSqlScope) sqliteWhere() (string, []any) {
	if s.Where == "" {
		s = DummyFlowgentSqlScope()
	}
	return s.Where, append([]any(nil), s.Args...)
}

func (s FlowgentSqlScope) postgresWhere(firstParameter int) (string, []any) {
	where, args := s.sqliteWhere()
	for index := range args {
		where = strings.Replace(where, "?", "$"+strconv.Itoa(firstParameter+index), 1)
	}
	return where, args
}
