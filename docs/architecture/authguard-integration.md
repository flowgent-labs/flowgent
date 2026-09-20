# AuthGuard Integration Boundary

Flowgent does not implement authentication, identity federation, sessions,
roles, policies, API keys, or authorization decisions. Those responsibilities
belong to AuthGuard and its Envoy Gateway integration.

The Flowgent API uses the official AuthGuard Go adapter SDK to verify the signed
access context delivered by the gateway and calls `CurrentScopeForAction` for
the HTTP operation, preventing a READ context from being reused for an update
or delete. The adapter compiles AuthGuard grants
for `urn:iam:prod:flowgent:global:<tenant>:namespace/<namespace>/**` into a
`FlowgentSqlScope` that embeds the official `model.SqlScope`; repositories
append it to business queries. Each repository declares its concrete resource
path (for example `flows/{agentflow_id}` or
`runs/{agentflow_run_id}/tasks/{id}`). The signed request `resource_urn`
selects the matching mapping before the SDK compiles allow and deny grants, so
aliases that share a table cannot authorize one another. Reads, updates, and
deletes include the compiled predicate; creates and upserts validate the
candidate row inside a query or transaction. When the
Helm integration is disabled, `FlowgentSqlScope` is a no-op so Flowgent has no
mandatory runtime dependency on AuthGuard.

The API Service is `ClusterIP` by default. Public clients must enter through the
AuthGuard-managed gateway and the protected `:9999` listener; missing, invalid,
expired, action-mismatched, or conflicting AuthGuard context always fails
closed. Controller, JM, TM, Sandbox, and Notifier use a separate `:9990`
control-plane listener with a dummy SDK scope. That listener implements no
Flowgent authentication and must never be attached to a public Gateway.

LDAP discovery, GitHub login, hosted login, principal lifecycle, roles,
policies, and audit decisions are configured and operated in AuthGuard. The
Flowgent Helm chart only controls whether the AuthGuard dependency is deployed
and supplies the adapter resource mapping and SDK signing-key environment.

[中文](authguard-integration_ZH.md)
