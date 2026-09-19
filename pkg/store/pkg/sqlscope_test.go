package store

import (
	"context"
	"reflect"
	"testing"
)

func TestFlowgentSqlScopeDefaultsToDummy(t *testing.T) {
	where, args := FlowgentSqlScopeFromContext(context.Background()).sqliteWhere()
	if where != "1=1" || len(args) != 0 {
		t.Fatalf("dummy scope = (%q, %#v), want (1=1, [])", where, args)
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
