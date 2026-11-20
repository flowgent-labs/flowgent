// Package authz adapts AuthGuard's verified request context to Flowgent's
// business repositories. Authentication and policy evaluation remain entirely
// inside AuthGuard.
package authz

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	guardaccess "authguard/adapters/golang/access"
	guardfilter "authguard/adapters/golang/filter"
	guardmodel "authguard/adapters/golang/model"
	guardutil "authguard/adapters/golang/util"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/storage/pkg"
)

const internalActor = "flowgent-system"

const (
	ActionRead    = "flowgent.api.read"
	ActionOperate = "flowgent.api.operate"
	ActionDelete  = "flowgent.api.delete"
)

// Adapter validates the signed context delivered by AuthGuard and translates
// its grants into the storage-owned FlowgentSqlScope boundary.
type Adapter struct {
	enabled bool
	filter  guardfilter.AccessFilter
	mapping guardmodel.ResourceSQLMapping
}

func NewAdapter(cfg config.AuthGuardAdapterConfig) (*Adapter, error) {
	adapter := &Adapter{enabled: cfg.Enabled}
	if !cfg.Enabled {
		return adapter, nil
	}

	partition := defaultValue(cfg.Partition, "prod")
	service := defaultValue(cfg.Service, "flowgent")
	region := defaultValue(cfg.Region, "global")
	tenant := strings.TrimSpace(cfg.Tenant)
	if tenant == "" {
		return nil, fmt.Errorf("authguard_adapter.tenant is required when the adapter is enabled")
	}

	resolver, err := guardaccess.NewHeaderAccessContextResolverFromEnv()
	if err != nil {
		return nil, fmt.Errorf("configure AuthGuard access-context resolver: %w", err)
	}
	adapter.filter = guardfilter.NewAccessFilter(resolver)
	adapter.mapping = guardmodel.ResourceSQLMapping{
		Partition: guardmodel.ConstSegment(partition),
		Service:   guardmodel.ConstSegment(service),
		Region:    guardmodel.ConstSegment(region),
		Tenant:    guardmodel.ConstSegment(tenant),
		Path: []guardmodel.PathMap{
			guardmodel.LiteralPath("namespace"),
			guardmodel.ColumnPath(`"namespace_id"`),
		},
	}
	return adapter, nil
}

// Middleware protects a business HTTP surface using the same fail-closed,
// action-bound SQL-scope flow as AuthGuard's official Go SQLx integration.
// Development deployments remain open only when the adapter is disabled.
func (a *Adapter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := storage.WithFlowgentSqlScope(r.Context(), storage.DummyFlowgentSqlScope())
		if !a.enabled || r.URL.Path == "/_/healthz" {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		resolved, authenticated, err := a.filter.Enter(ctx, guardfilter.HeaderFunc(r.Header.Get))
		if err != nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if !authenticated {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		action, ok := actionForMethod(r.Method)
		if !ok {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		scope, err := guardaccess.CurrentScopeForAction(resolved, action, a.mapping)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		resolved = storage.WithFlowgentSqlScope(resolved, storage.NewFlowgentSqlScope(scope))
		requestAccess, _ := guardaccess.RequestAccess(resolved)
		requestResource, err := guardutil.ParseURNPattern(requestAccess.ResourceURN)
		if err != nil || len(requestResource.Path) < 2 || requestResource.Path[0] != "namespace" {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		resolved = storage.WithFlowgentSqlScopeResolver(resolved, requestResource.Path[2:], func(path storage.FlowgentResourcePath) (guardmodel.SqlScope, error) {
			mapping := a.mapping
			mapping.Path = append(append([]guardmodel.PathMap(nil), a.mapping.Path...), path...)
			return guardaccess.ScopeForAction(requestAccess, action, mapping)
		})
		next.ServeHTTP(w, r.WithContext(resolved))
	})
}

// InternalMiddleware marks the trusted Flowgent control-plane listener with a
// dummy SDK scope. It performs no authentication and must never be exposed by
// an Internet-facing Service or Gateway.
func (a *Adapter) InternalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := storage.WithFlowgentSqlScope(r.Context(), storage.DummyFlowgentSqlScope())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func actionForMethod(method string) (string, bool) {
	switch method {
	case http.MethodGet, http.MethodHead:
		return ActionRead, true
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return ActionOperate, true
	case http.MethodDelete:
		return ActionDelete, true
	default:
		return "", false
	}
}

// PrincipalID returns the principal verified by the AuthGuard SDK. Internal
// control-plane requests intentionally use a stable system actor.
func PrincipalID(r *http.Request) string {
	return PrincipalIDFromContext(r.Context())
}

func PrincipalIDFromContext(ctx context.Context) string {
	requestAccess, ok := guardaccess.RequestAccess(ctx)
	if !ok || strings.TrimSpace(requestAccess.PrincipalID) == "" {
		return internalActor
	}
	return requestAccess.PrincipalID
}

func defaultValue(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
