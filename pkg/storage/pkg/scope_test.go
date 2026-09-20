package storage

import (
	"context"
	"reflect"
	"testing"

	guardmodel "authguard/adapters/golang/model"
)

func TestFlowgentSqlScopeEmbedsOfficialSDKScope(t *testing.T) {
	sdk := guardmodel.SqlScope{Where: `"namespace_id" = ?`, Args: []any{"security-fixer"}}
	scope := NewFlowgentSqlScope(sdk)
	if !reflect.DeepEqual(scope.AuthGuardSqlScope(), sdk) {
		t.Fatalf("unwrapped scope = %#v, want %#v", scope.AuthGuardSqlScope(), sdk)
	}
	sdk.Args[0] = "mutated"
	if scope.Args[0] != "security-fixer" {
		t.Fatal("FlowgentSqlScope did not defensively copy SDK arguments")
	}
}

func TestFlowgentSqlScopeDefaultsToDummy(t *testing.T) {
	where, args := FlowgentSqlScopeFromContext(context.Background()).sqliteWhere()
	if where != "1=1" || len(args) != 0 {
		t.Fatalf("dummy scope = (%q, %#v), want (1=1, [])", where, args)
	}
}

func TestFlowgentSqlScopeForTableUsesCentralRegistry(t *testing.T) {
	var resolvedPath FlowgentResourcePath
	ctx := WithFlowgentSqlScopeResolver(
		context.Background(),
		[]string{"flows", "payment-reconciliation"},
		func(path FlowgentResourcePath) (guardmodel.SqlScope, error) {
			resolvedPath = append(FlowgentResourcePath(nil), path...)
			return guardmodel.SqlScope{
				Where: `"agentflow_id" = ?`,
				Args:  []any{"payment-reconciliation"},
			}, nil
		},
	)

	scope := FlowgentSqlScopeForTable(ctx, "orh_agentflow")
	where, args := scope.SQLiteWhere()
	if where != `"agentflow_id" = ?` || !reflect.DeepEqual(args, []any{"payment-reconciliation"}) {
		t.Fatalf("scope = (%q, %#v)", where, args)
	}
	if len(resolvedPath) != 2 || resolvedPath[0].Kind != guardmodel.PathLiteral || resolvedPath[0].Value != "flows" || resolvedPath[1].Kind != guardmodel.PathColumn || resolvedPath[1].Value != `"agentflow_id"` {
		t.Fatalf("resolved path = %#v, want flows/{agentflow_id}", resolvedPath)
	}
}

func TestFlowgentSqlScopeForUnknownTableFailsClosedWithResolver(t *testing.T) {
	resolverCalled := false
	ctx := WithFlowgentSqlScopeResolver(
		context.Background(),
		[]string{"flows", "payment-reconciliation"},
		func(FlowgentResourcePath) (guardmodel.SqlScope, error) {
			resolverCalled = true
			return guardmodel.SqlScope{Where: "1=1"}, nil
		},
	)

	where, _ := FlowgentSqlScopeForTable(ctx, "unregistered_table").SQLiteWhere()
	if where != "0=1" {
		t.Fatalf("where = %q, want deny-all", where)
	}
	if resolverCalled {
		t.Fatal("resolver must not run without a registered table mapping")
	}
}

func TestFlowgentSqlScopeForUnknownTableRemainsDummyWithoutResolver(t *testing.T) {
	where, args := FlowgentSqlScopeForTable(context.Background(), "unregistered_table").SQLiteWhere()
	if where != "1=1" || len(args) != 0 {
		t.Fatalf("standalone scope = (%q, %#v), want (1=1, [])", where, args)
	}
}

func TestNamespaceFlowgentSqlScopeRebindsPostgresArguments(t *testing.T) {
	scope := NamespaceFlowgentSqlScope("security-fixer")
	where, args := scope.postgresWhere(3)
	if where != `"namespace_id" = $3` {
		t.Fatalf("where = %q", where)
	}
	if !reflect.DeepEqual(args, []any{"security-fixer"}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestEmptyNamespaceFailsClosed(t *testing.T) {
	where, _ := NamespaceFlowgentSqlScope(" ").sqliteWhere()
	if where != "0=1" {
		t.Fatalf("where = %q, want deny-all", where)
	}
}
