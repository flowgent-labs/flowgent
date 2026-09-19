# Enterprise IAM Authorization Architecture

**Status:** Architecture Baseline
**Date:** 2026-08-21
**Scope:** Enterprise IAM / App Gateway / API Server / Flowgent / Sigbot Core
**Current landing:** Embedded in the Flowgent Go API Server
**Future reuse:** The same model can be implemented in the `../sigbot-core` Rust project

## 1. Summary

This document defines a generic, standalone, enterprise-grade identity and
authorization architecture. It is not bound to any single business system and
does not make Flowgent `namespace` or `agent flow` core IAM concepts. A business
system only needs to expose its objects as protected resources, then authorize
them through Resource URNs, actions, roles, grants, conditions, and business
resource adapters.

Core decisions:

- Authentication answers "who is calling".
- Authorization answers "which action can the caller perform on which resource".
- External GitHub/OIDC/LDAP identities are bindings, not business authorization
  principals.
- `iam_subject` represents users, service accounts, workloads, and system
  subjects. `iam_group` represents teams or collection principals.
- Runtime evaluation uses `PrincipalSet = subject + groups`.
- Actions use `iam_action`, roles use `iam_role`, and authorization facts use
  `iam_grant`.
- Resource identity uses an internal `urn:iam:...` Resource URN based on the
  RFC 8141 URN syntax style.
- IAM core does not maintain an `iam_resource` table. Business tables remain the
  source of truth for resource existence, attributes, and lifecycle.
- Request tuples are route/resource matchers, not the resource permission model.
- Resource listing is handled by business Resource Adapters that compile
  effective grants into SQL scopes.

In large enterprises this model usually belongs in an App Gateway / IAM
microservice. In open-source deployments it can be embedded in an API Server to
reduce operational complexity. Both shapes MUST preserve the same model,
decision algorithm, and audit semantics.

### 1.1 A minimal authorization story

Alice signs in through GitHub. GitHub proves "this is Alice"; it does not decide
what Alice can access. The system binds the GitHub identity to an internal
subject:

```text
github:29530154
  -> iam_subject:alice
```

Alice belongs to `security-team`:

```text
iam_subject:alice
  -> iam_group_member
  -> iam_group:security-team
```

An administrator grants access to `security-team`:

```text
iam_group:security-team
  -> iam_grant
       urn = urn:iam:prod:flowgent:global:security:agent-flow/*
  -> iam_grant_role
  -> iam_role:reader
  -> iam_role_action
  -> iam_action:agentflow.read
  -> iam_action:agentflow.trace.read
```

When Alice requests a flow trace:

```text
GET /api/v1/security/flows/security-autonomy-fixer/runs/run-123/trace
```

The route matcher produces:

```text
action       = agentflow.trace.read
resourceURN  = urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer/run/run-123
parentURNs   = [
  urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer,
  urn:iam:prod:flowgent:global:security:namespace/security,
  urn:iam:prod:flowgent:global:platform:platform/root
]
```

The evaluator finds that `security-team` has the `reader` role, the role contains
`agentflow.trace.read`, and the grant pattern covers the flow, so the request is
allowed.

## 2. Goals

### 2.1 Functional goals

- Support GitHub, OIDC, LDAP, password, API key, and workload tokens.
- Support subjects, groups, roles, grants, action catalogs, and route/resource
  matchers.
- Support platform, tenant/domain, and resource-level authorization.
- Support a GitHub-like organization/repository authorization experience without
  binding the model to GitHub concepts.
- Support arbitrary business resource types such as repositories, agent flows,
  bots, workflows, datasets, and channels.
- Support hierarchical inheritance, such as a namespace owner inheriting access
  to agent flows under that namespace.
- Enforce authorization in backend middleware or gateway code.
- Expose UI auth context for menus, buttons, and access-denied states.
- Audit authentication, authorization decisions, and authorization management.

### 2.2 Engineering goals

- High cohesion: authn, identity resolution, matchers, evaluators, and audit
  remain separate modules.
- Low coupling: IAM core does not depend on business table schemas.
- No duplicate resource truth: IAM does not mirror business resources into a
  core resource table.
- Cross-language portability: Go and Rust implementations share model semantics,
  decision algorithms, and golden fixtures.
- Flexible deployment: embedded API Server for Flowgent, extractable
  Gateway/IAM service for enterprise deployments.

### 2.3 Non-goals

- UI hiding is not a security boundary.
- External provider groups are not final authorization principals.
- Request method/path/query is not the resource permission model.
- IAM core does not own business resource lifecycle.
- Arbitrary regex resource patterns are not supported in v1.
- Legacy compatibility models are not retained.

## 3. Overall model

Unified authorization chain:

```text
external identity
  -> iam_subject
  -> PrincipalSet(subject + groups)
  -> iam_grant(urn + conditions)
  -> iam_grant_role
  -> iam_role
  -> iam_role_action
  -> iam_action
  -> authorization decision
```

Single-request decision chain:

```text
HTTP request
  -> authn middleware
  -> route/resource matcher
  -> action identifier
  -> resource URN + parent resource URNs
  -> effective grants
  -> role/action check
  -> condition check
  -> ALLOW / DENY
```

Resource-listing chain:

```text
current subject
  -> effective grants for the requested action
  -> urns
  -> business Resource Adapter
  -> SQL predicate / query scope
  -> business DB
```

### 3.1 Core relationship map

```text
                         ┌─────────────────────┐
external identity ─────▶ │     iam_subject      │
                         └──────────┬──────────┘
                                    │ membership
                                    ▼
                         ┌─────────────────────┐
                         │      iam_group       │
                         └──────────┬──────────┘
                                    │ grantee
                                    ▼
┌──────────────────────────────────────────────────────────────┐
│                         iam_grant                            │
│  grantee_type + grantee_id + urn + condition │
└──────────────────────────────┬───────────────────────────────┘
                               │ grant roles
                               ▼
                         ┌──────────────┐
                         │   iam_role   │
                         └──────┬───────┘
                                │ role actions
                                ▼
                         ┌──────────────┐
request ── matcher ───▶  │  iam_action  │
                         └──────┬───────┘
                                │
                                ▼
                         decision: ALLOW / DENY
```

Only the business system knows its resource tables. IAM sees Resource URNs and
actions; business Resource Adapters translate grant patterns into query scopes.

## 4. Core concepts

### 4.1 Subject

A Subject is an internal principal that can be authenticated, audited, and
directly granted access. It can be a human user, service account, workload
identity, or system subject.

The table is named `iam_subject` instead of `iam_user` because `user` only
covers human users and does not express service accounts, workloads, or system
identities.

External identities do not directly participate in business authorization.
GitHub/OIDC/LDAP login results only resolve or bind an internal `iam_subject`.

### 4.2 Identity

Identity is the login source. To avoid duplicate fields across subject and
identity tables, identities are stored inside `iam_subject.identities JSONB`.

Example:

```json
[
  {"provider": "github", "issuer": "github", "subject": "29530154"},
  {"provider": "oidc", "issuer": "https://idp.example.com", "subject": "00u123"},
  {"provider": "ldap", "issuer": "corp-ad", "subject": "CN=james,OU=Users,DC=corp,DC=example"}
]
```

Identity entries only store binding data. They MUST NOT duplicate email, name,
avatar, profile, or last-login fields.

### 4.3 Group

`iam_group` is a scoped team or collection principal. Its scope can be an
organization, tenant, namespace, project, business unit, or custom business
domain.

Groups SHOULD NOT be merged into `iam_subject`:

- Subjects are authenticatable entities; groups are collection entities.
- Subjects need identity, email, service-account, and workload constraints.
- Groups need scope, name, and membership constraints.
- Merging creates many nullable fields or self-referential memberships.
- In enterprise RBAC, groups often receive roles but requests are still issued
  by concrete subjects.

The database keeps `iam_subject` and `iam_group` separate. Runtime evaluation
unifies them as:

```text
PrincipalSet = authenticated subject + direct groups + inherited groups
```

v1 does not support nested groups. Group membership is only
`iam_subject -> iam_group`. Do not introduce `iam_group_edge` until a real
business requirement needs nested teams; if it is added later, it must enforce
DAG semantics, maximum depth, and cycle detection.

### 4.4 Action

Action means "what to do" and uses dot-separated names:

```text
resource.read
resource.write
resource.delete
resource.access.manage
resource.run.trigger
resource.trace.read
domain.member.manage
platform.admin
```

The table is named `iam_action`, not `iam_permission`. Permission is the runtime
authorization result; action is the atomic operation that roles compose.

Resource targets are not encoded in action strings. They are resolved by
route/resource matchers and grants.

### 4.5 Role

Role is a collection of actions. Normal grants SHOULD assign roles instead of
loose individual actions.

Recommended built-in roles:

| Role | Scope | Capability |
|---|---|---|
| owner | resource/domain | Full management, including access |
| maintainer | resource/domain | Manage resources, but not access owners |
| writer | resource | Modify resources and trigger execution |
| operator | resource | Execute, cancel, approve, but not modify definitions |
| reader | resource/domain | Read only |
| auditor | platform/domain | Read-only audit and evidence access |

### 4.6 Grant

Grant is an append-only authorization fact:

```text
grantee(subject/group)
  has roles
  on resource(urn)
  under conditions
```

A grant can bind one or more roles through `iam_grant_role`.

### 4.7 Resource URN

A protected resource is any business object. IAM identifies resources with
Resource URNs.

The internal format follows the RFC 8141 URN syntax style:

```text
urn:iam:<partition>:<service>:<region>:<tenant>:<resource-path>
```

Segments:

| Segment | Meaning |
|---|---|
| `iam` | Internal URN namespace identifier |
| `partition` | Management partition or environment, such as `prod`, `staging`, `corp` |
| `service` | Business system or microservice, such as `flowgent`, `sigbot` |
| `region` | Region; use `global` for non-regional resources |
| `tenant` | Stable isolation boundary such as tenant, organization, namespace, or account |
| `resource-path` | Business-defined path, recommended as `<type>/<id>[/<subtype>/<id>]` |

Examples:

```text
urn:iam:prod:flowgent:global:default:namespace/default
urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer
urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer/run/run-123
urn:iam:prod:sigbot:global:sales:bot/customer-support
urn:iam:prod:sigbot:global:sales:dataset/customer-faq
```

Wildcard pattern examples:

```text
urn:iam:prod:flowgent:global:default:agent-flow/*
urn:iam:prod:flowgent:global:default:**
urn:iam:prod:sigbot:global:sales:bot/*
urn:iam:prod:*:global:*:**
```

v1 wildcard rules MUST remain predictable, compilable, and query-pushdown
friendly:

- Exact segments match exactly.
- `*` matches one colon segment or one resource-path segment.
- `**` is allowed only at the end of `resource-path` and matches a subtree.
- Arbitrary regex is not supported.
- Partial segment globbing such as `foo*bar` is not supported.

### 4.8 URN, ARN, and request tuples

ARN is the AWS resource naming convention. This design borrows the resource
locator idea but does not reuse the `arn:` prefix, because that would imply AWS
ARN compatibility.

RFC 8141 defines the outer URN syntax as `urn:<NID>:<NSS>`. This design uses
`iam` as an internal NID and defines fixed NSS segments. If public
cross-organization interoperability is required later, a formal NID registration
or explicit namespace compatibility statement is required.

Request tuples such as method, URI/path, query params, and path params are only
used by route/resource matchers:

```text
request tuple
  -> action
  -> resource URN
  -> parent resource URNs
```

They do not replace Resource URNs. Source IP, method, query, MFA, and time are
conditions and belong in `conditions_json`.

### 4.9 Why `parentUrns` exists

A request usually targets a leaf resource, but authorization is often granted on
a parent scope.

Examples:

```text
GitHub:     repo inherits from org
Flowgent:   flow/run inherits from flow and namespace
S3:         object inherits from bucket or access point
```

If the matcher returned only the leaf Resource URN, an org/namespace/bucket
grant would require one of two bad designs:

- materialize grants to every child resource;
- make the evaluator query business tables to discover parents.

Both break the goal of keeping IAM core independent from business data. Instead,
the route/resource matcher returns a deterministic chain:

```text
resourceUrn = concrete leaf resource
parentUrns  = concrete parent resources, nearest first
```

Example:

```text
resourceUrn = urn:iam:prod:flowgent:global:security:agent-flow/fixer/run/run-123
parentUrns  = [
  urn:iam:prod:flowgent:global:security:agent-flow/fixer,
  urn:iam:prod:flowgent:global:security:namespace/security,
  urn:iam:prod:flowgent:global:platform:platform/root
]
```

`parentUrns` MUST contain concrete URNs, not wildcard patterns. Wildcards belong
only in `iam_grant.urn`. The evaluator checks the grant pattern against
`[resourceUrn] + parentUrns`.

This keeps inheritance explicit, avoids grant expansion, and lets each business
service define its own parent chain without coupling IAM core to business
tables.

### 4.10 Why IAM core has no resource table

IAM core does not maintain an `iam_resource` table. Resource existence,
attributes, and lifecycle are owned by business tables, such as Flowgent
namespace/flow tables or Sigbot bot/channel/dataset tables.

Reasons:

- Avoid dual-write inconsistency between IAM resource tables and business
  resource tables.
- Avoid binding IAM core to concrete business schemas.
- Avoid deleted resource leakage caused by stale IAM resource rows.
- Allow different services to protect different resource types with the same IAM
  model.

IAM stores only authorization targets:

```text
iam_grant.urn
```

If a project needs resource search, grant pickers, cross-service inventory, or
offline audit acceleration, it MAY add an optional projection table:

```text
iam_resource_projection
```

This table is a cache/index only. It MUST NOT participate in authorization
correctness; projection staleness MUST NOT cause privilege escalation.

## 5. Data model

### 5.1 `iam_subject`

```text
id
kind            -- user / service_account / workload / system
email           -- human-user email; nullable for non-human subjects
name
avatar_url
identities      -- JSONB array
status
authz_version
created_at
updated_at
```

Constraints:

- For `kind=user`, `email` is the primary human-user identifier and should use a
  partial unique index.
- Non-human subjects do not require email.
- `(provider, issuer, subject)` inside `identities` is globally unique.
- `last_login_at` is not stored on the subject. Login events go to audit.
- Identity entries do not duplicate email/name/avatar/profile.
- `authz_version` invalidates or refreshes stale auth contexts after changes.

JSONB array uniqueness requires resolver-level transaction protection:

```text
1. identity_key = provider + "\0" + issuer + "\0" + subject
2. pg_advisory_xact_lock(hash(identity_key))
3. SELECT subject WHERE identities @> identity
4. if found return subject
5. else SELECT subject WHERE kind='user' AND email = verified_email
6. append identity or create subject
7. commit
```

### 5.2 `iam_group`

```text
id
scope_type      -- organization / tenant / namespace / project / custom
scope_key
name
description
status
created_at
updated_at
```

Constraint:

```text
unique(scope_type, scope_key, name)
```

### 5.3 `iam_group_member`

```text
id
group_id
subject_id
created_at
created_by
deleted_at
deleted_by
```

Deleting a membership revokes it. `deleted_at/deleted_by` are retained for audit
and recovery analysis.

### 5.4 `iam_action`

`iam_action` stores action identifiers and HTTP route/resource matchers.

```text
identifier      -- primary key, for example resource.read
matchers        -- JSONB array
```

Matcher element:

```json
{
  "method": "GET",
  "uri": "^/api/v1/([^/]+)/flows/([^/]+)$",
  "path": {"namespace": 1, "flow": 2},
  "queryParams": {},
  "urn": "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}",
  "parentUrns": [
    "urn:iam:prod:flowgent:global:{namespace}:namespace/{namespace}",
    "urn:iam:prod:flowgent:global:root:platform/root"
  ]
}
```

Rules:

- `identifier` is globally unique.
- `matchers` contains all route matchers protected by the action.
- `method`, `uri`, `queryParams`, and `path` are not split into separate columns.
- `path` maps regex capture groups to names.
- `urn` supports template variables.
- `parentUrns` lists concrete parent resource URNs used for inheritance checks.
- `matchers=[]` means the action is internal or role-composition only and does
  not directly match HTTP routes.

### 5.5 `iam_role`

```text
id
name
scope_type       -- platform / domain / resource
builtin
description
status
created_at
updated_at
```

### 5.6 `iam_role_action`

```text
role_id
action_identifier
```

Constraint:

```text
unique(role_id, action_identifier)
```

### 5.7 `iam_grant`

```text
id
grantee_type                 -- SUBJECT / GROUP
grantee_id
urn
effect                       -- ALLOW / DENY
conditions_json
created_at
created_by
deleted_at
deleted_by
status
```

Constraints:

- `grantee_type=SUBJECT` references `iam_subject.id`.
- `grantee_type=GROUP` references `iam_group.id`.
- `urn` is the authorization target. It may be an exact Resource URN or a
  limited Resource URN pattern. The short name is intentional because the table
  is already `iam_grant`.
- Active duplicate grants should be avoided.
- Grants are append-only and are not updated in place.
- DENY grants take precedence over ALLOW grants.

### 5.8 `iam_grant_role`

```text
grant_id
role_id
```

Constraint:

```text
unique(grant_id, role_id)
```

When a grant is revoked, its role links become inactive with the grant.

### 5.9 `iam_api_key`

```text
id
name
owner_subject_id
secret_hash
prefix
suffix
allowed_urns
allowed_actions
expires_at
revoked_at
created_at
created_by
```

API key plaintext is shown only once. The database stores only a hash. API keys
can attenuate the owner subject's permissions but cannot expand them.

### 5.10 `iam_audit_event`

```text
id
actor_type
actor_id
action
resource_urn
decision
reason
grant_id
role_ids
request_id
source_ip
metadata_json
created_at
```

Audit events are append-only. Secrets, tokens, and credentials MUST NOT enter
audit metadata.

## 6. Authorization decision

### 6.1 Request algorithm

```text
1. Authn middleware verifies the caller.
2. Resolver loads iam_subject.
3. Group resolver builds PrincipalSet(subject + groups).
4. Route matcher finds action/resourceUrn/parentUrns from iam_action.matchers.
5. Evaluator loads subject direct grants.
6. Evaluator loads group grants.
7. Evaluator expands grant roles and role actions.
8. Evaluator checks action match.
9. Evaluator checks urn against resourceUrn and parentUrns.
10. Evaluator checks conditions_json.
11. Explicit DENY wins.
12. Default deny.
13. Write audit event.
```

### 6.2 Core pseudocode

Single-request authorization can be reduced to this pseudocode:

```text
function Authorize(request):
    subject = Authenticate(request)
    if subject is None:
        return DENY("unauthenticated")

    principals = PrincipalSet(subject, GroupsOf(subject))

    match = MatchAction(request.method, request.path, request.query)
    if match is None:
        return DENY("no matching protected action")

    resource_urns = [match.resourceURN] + match.parentURNs
    grants = LoadActiveGrants(principals)

    decision = DENY("default deny")

    for grant in grants:
        if not AnyPatternMatches(grant.urn, resource_urns):
            continue

        role_actions = ActionsOfGrantRoles(grant.id)
        if match.action not in role_actions:
            continue

        if not ConditionsMatch(grant.conditions_json, request, subject, match.resourceURN):
            continue

        if grant.effect == "DENY":
            return DENY("explicit deny", grant.id)

        decision = ALLOW("matched grant", grant.id)

    return decision
```

This pseudocode intentionally does not read business resource tables. Business
handlers or Resource Adapters decide whether the resource exists. The evaluator
only decides whether the current principal may access the Resource URN if it
exists.

### 6.3 Effective grants

Effective authorization comes from:

```text
subject direct grants
+ subject group grants
+ API key attenuation scope
```

Resource inheritance is not materialized as extra grants. It is evaluated by
matching grant patterns against `resourceUrn` and `parentUrns`.

### 6.4 Conditions

`conditions_json` expresses ABAC conditions, not resource identity.

Example:

```json
{
  "sourceIp": {"inCidr": ["10.0.0.0/8"]},
  "request": {"methods": ["GET", "POST"]},
  "time": {"before": "2026-12-31T23:59:59Z"},
  "subject": {"mfa": true},
  "resource": {"tags": {"environment": "prod"}}
}
```

Attribute sources:

- Request attributes come from middleware.
- Subject attributes come from `iam_subject` or auth context.
- Resource attributes come from the business Resource Adapter.
- Environment attributes come from deployment or policy context.

If a required condition attribute cannot be obtained reliably, the condition
does not match.

## 7. Resource listing and Resource Adapters

Request interception answers "can this request execute". Enterprise systems
also need "which resources can the user see".

IAM does not list resources. The business service lists resources and compiles
IAM authorization scopes into its query.

### 7.1 Resource Adapter interface

Each business resource type implements a thin adapter:

```text
BuildURN(row) -> resource_urn
BuildParentURNs(row) -> []resource_urn
CompileListScope(action, effective_grants) -> SQL predicate / query scope
LoadResourceAttributes(resource_urn) -> attributes
```

Responsibilities:

- IAM core computes effective grants for the subject/group set.
- Resource Adapter understands business tables and compiles
  `urn` into query scopes.
- Business DB remains the source of truth for existence, attributes, and
  lifecycle.

Resource-listing pseudocode:

```text
function ListResources(subject, action, resourceType, filters):
    principals = PrincipalSet(subject, GroupsOf(subject))
    grants = LoadActiveGrants(principals)

    allowed_patterns = []
    denied_patterns = []

    for grant in grants:
        if action not in ActionsOfGrantRoles(grant.id):
            continue

        if not ConditionsMatchForList(grant.conditions_json, subject):
            continue

        if grant.effect == "DENY":
            denied_patterns.append(grant.urn)
        else:
            allowed_patterns.append(grant.urn)

    scope = ResourceAdapter(resourceType).CompileListScope(
        allow = allowed_patterns,
        deny  = denied_patterns,
        filters = filters
    )

    return BusinessDB.Query(resourceType, scope)
```

The key rule is that IAM emits grant patterns, while business adapters emit SQL
predicates. IAM must not scan business tables directly.

### 7.2 Flowgent query example

User grants:

```text
reader on urn:iam:prod:flowgent:global:default:agent-flow/security-autonomy-fixer
reader on urn:iam:prod:flowgent:global:security:agent-flow/*
owner  on urn:iam:prod:flowgent:global:platform:platform/root
```

Flowgent's flow adapter can compile them into:

```sql
WHERE
  (namespace = 'default' AND name = 'security-autonomy-fixer')
  OR
  (namespace = 'security')
  OR
  (:has_platform_owner = true)
```

If a user has direct permission on one flow but is not namespace owner, list
flows should still include that flow. This mirrors the GitHub behavior where an
individually granted repository appears in the repository list.

### 7.3 Pattern pushdown limits

To support stable SQL pushdown, v1 grant `urn` patterns support only:

```text
exact
*
trailing /*
trailing /**
```

Arbitrary regex is not supported. Otherwise evaluation degrades to full scan
plus application filtering, which is not acceptable for enterprise systems.

### 7.4 Consistency strategy

Business tables are the source of truth, so there is no dual-write consistency
problem between IAM resources and business resources.

Grant creation can use either strategy:

- Strong validation: call the business Resource Adapter before creating a grant.
- Weak validation: allow grants for future resources.

After a resource is deleted, grants may be cleaned asynchronously. Lists come
from business tables, so deleted resources are not shown because a stale grant
exists. Requests still return not found from business handlers.

## 8. Authorization scenarios

### 8.1 Team inherits access to every flow in one namespace

Grant:

```text
iam_group:security-team
  -> grant reader
  -> urn:iam:prod:flowgent:global:security:agent-flow/*
```

Request:

```text
GET /api/v1/security/flows/security-autonomy-fixer
```

Matcher:

```text
action      = agentflow.read
resourceURN = urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer
```

Decision:

```text
ALLOW
```

Reason: the grant pattern covers the flow and the reader role contains
`agentflow.read`.

### 8.2 A user has access to one flow and must see it in list results

Grant:

```text
iam_subject:bob
  -> grant reader
  -> urn:iam:prod:flowgent:global:default:agent-flow/payment-risk-fixer
```

Bob is not owner of the `default` namespace. The Flowgent Resource Adapter still
compiles list scope as:

```sql
WHERE namespace = 'default'
  AND name = 'payment-risk-fixer'
```

Result: Bob sees `payment-risk-fixer`, but not other flows in the same
namespace.

### 8.3 Read access cannot trigger runs

Grant:

```text
iam_group:auditors
  -> grant reader
  -> urn:iam:prod:flowgent:global:default:agent-flow/*
```

Request:

```text
POST /api/v1/default/flows/security-autonomy-fixer/runs
```

Matcher:

```text
action = agentflow.run.trigger
```

Decision:

```text
DENY
```

Reason: the reader role does not contain `agentflow.run.trigger`, even though
the Resource URN matches.

### 8.4 Explicit DENY wins

Grants:

```text
iam_group:dev-team
  -> grant writer
  -> urn:iam:prod:flowgent:global:default:agent-flow/*

iam_subject:carol
  -> grant DENY writer
  -> urn:iam:prod:flowgent:global:default:agent-flow/payroll-fixer
```

Carol can write other flows in the `default` namespace, but cannot write
`payroll-fixer`.

### 8.5 API keys only attenuate access

Alice is namespace owner. She creates an API key:

```text
allowed_actions = [agentflow.run.trigger]
allowed_urns = [
  urn:iam:prod:flowgent:global:security:agent-flow/security-autonomy-fixer
]
```

The key can only trigger that one flow. Even if Alice has broader permissions,
the key cannot access other flows, read secrets, or manage access.

### 8.6 Deleted resource with stale grant

A grant still exists:

```text
grant reader on urn:iam:prod:flowgent:global:default:agent-flow/old-flow
```

But `old-flow` has been deleted from the business flow table.

Result:

- list flows does not show `old-flow`, because lists come from business tables.
- direct requests return not found.
- asynchronous cleanup may delete the stale grant, but the stale grant does not
  create privilege escalation.

### 8.7 GitHub-like organization and repository authorization

GitHub organization/repository access is hierarchical: an organization owns
repositories; teams or users receive repository roles; organization-level
settings can apply broad defaults. The same shape maps directly to this IAM
model.

```text
GitHub org       -> tenant/domain
GitHub team      -> iam_group
GitHub repo      -> Resource URN
GitHub repo role -> iam_role
```

Example URNs:

```text
urn:iam:prod:github:global:flowgent-labs:org/flowgent-labs
urn:iam:prod:github:global:flowgent-labs:repo/flowgent
urn:iam:prod:github:global:flowgent-labs:repo/flowgent-ui
```

Team grant:

```text
iam_group:platform-team
  -> grant maintainer
  -> urn:iam:prod:github:global:flowgent-labs:repo/flowgent
```

Organization-wide grant:

```text
iam_group:security-reviewers
  -> grant reader
  -> urn:iam:prod:github:global:flowgent-labs:repo/*
```

List repositories compiles to:

```sql
WHERE org = 'flowgent-labs'
  AND (
    repo = 'flowgent'
    OR :has_all_org_repo_read = true
  )
```

This is the same model used for Flowgent namespaces and flows. The resource
names differ, but subject/group/role/grant/action semantics do not.

### 8.8 AWS S3-style cross-region resource authorization

S3 is a different shape from GitHub: bucket names are global, object keys are
paths, access points can be regional, and Multi-Region Access Points provide a
global endpoint over buckets in multiple regions. The same URN model still
works because region and resource-path are first-class segments.

Bucket and object examples:

```text
urn:iam:prod:s3:global:111122223333:bucket/company-audit-logs
urn:iam:prod:s3:global:111122223333:bucket/company-audit-logs/object/2026/08/22/report.json
```

Regional access point:

```text
urn:iam:prod:s3:us-west-2:111122223333:access-point/audit-reader
urn:iam:prod:s3:us-west-2:111122223333:access-point/audit-reader/object/*
```

Multi-region access point:

```text
urn:iam:prod:s3:global:111122223333:multi-region-access-point/audit-global/object/*
```

Grant:

```text
iam_group:global-auditors
  -> grant reader
  -> urn:iam:prod:s3:*:111122223333:access-point/audit-reader/object/**
```

Condition:

```json
{
  "sourceIp": {"inCidr": ["10.0.0.0/8"]},
  "request": {"tls": true}
}
```

The important point is that region is just one URN segment. GitHub-like resources
can use `global`; S3-like resources can use concrete regions or `global` for
global endpoints. The evaluator remains unchanged.

## 9. Authentication and subject resolution

### 9.1 GitHub OAuth

```text
/auth/login/github
  -> GitHub OAuth authorize
/auth/callback/github
  -> oauth2.Exchange
  -> GitHub /user + /user/emails
  -> resolve iam_subject by identities[github/github/user_id]
  -> fallback to verified primary email resolve/create iam_subject(kind=user)
  -> append github identity if needed
  -> compute auth context
  -> issue internal JWT/session
  -> Set-Cookie HttpOnly
  -> redirect
```

### 9.2 OIDC

```text
OIDC discovery
  -> authorization code
  -> token exchange
  -> ID token verify
  -> nonce validate
  -> resolve iam_subject by identities[oidc/issuer/sub]
  -> fallback to verified email resolve/create iam_subject(kind=user)
  -> append oidc identity if needed
```

### 9.3 LDAP

```text
service bind
  -> user search
  -> user password bind
  -> resolve iam_subject by identities[ldap/domain/subject]
  -> fallback to LDAP email resolve/create iam_subject(kind=user)
  -> append ldap identity if needed
```

LDAP subject source is configurable. Defaults can include:

```text
dn
uid
sAMAccountName
userPrincipalName
```

## 10. JWT, Cookie, and UI auth context

Browser login uses an HttpOnly session cookie by default. JavaScript does not
read JWTs.

JWT may contain:

```json
{
  "sub": "iam-subject-id",
  "email": "user@example.com",
  "preferred_username": "user@example.com",
  "identity_provider": "github",
  "actions": ["resource.read", "resource.run.trigger"],
  "authz_version": 12
}
```

The UI does not parse JWTs. It calls:

```text
GET /api/v1/auth/me
```

Response:

```json
{
  "subject": {},
  "identity": {},
  "actions": [],
  "accessible_urns": [],
  "authz_version": 12
}
```

The UI uses this result for menus and buttons. Backend middleware or gateway
authorization is the only trusted security boundary.

## 11. Bootstrap administrator

To avoid successful first SSO login with no permissions, bootstrap admin emails
are supported:

```yaml
auth:
  bootstrap_admin_emails:
    - admin@example.com
```

If verified email matches:

```text
resolve/create iam_subject(kind=user)
append identity
grant platform owner
```

The process MUST be idempotent. Production deployments should disable or narrow
the bootstrap list after initial authorization.

## 12. Flowgent mapping example

| Flowgent concept | Generic IAM concept |
|---|---|
| namespace | tenant / resource domain |
| agent flow | protected resource |
| flow run / task / trace | child resource or execution evidence under agent-flow |
| LLM provider / MCP / skill / notification channel | other protected resources |

Route matcher example:

```json
{
  "method": "GET",
  "uri": "^/api/v1/([^/]+)/flows/([^/]+)/runs/([^/]+)/trace$",
  "path": {"namespace": 1, "flow": 2, "run": 3},
  "queryParams": {},
  "urn": "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}/run/{run}",
  "parentUrns": [
    "urn:iam:prod:flowgent:global:{namespace}:agent-flow/{flow}",
    "urn:iam:prod:flowgent:global:{namespace}:namespace/{namespace}",
    "urn:iam:prod:flowgent:global:platform:platform/root"
  ]
}
```

`agent-flow` is only an example protected resource type. It is not built into
the IAM model.

## 13. Sigbot Core mapping example

| Sigbot concept | Generic IAM concept |
|---|---|
| organization / team | tenant / resource domain |
| bot | protected resource |
| skill / tool / channel / memory | protected resource or bot child resource |
| conversation / evidence | evidence resource under bot or channel |

Examples:

```text
urn:iam:prod:sigbot:global:sales:bot/customer-support
urn:iam:prod:sigbot:global:sales:bot/customer-support/memory/customer-faq
urn:iam:prod:sigbot:global:sales:channel/slack-main
```

## 14. Module boundaries

### 14.1 Go / Flowgent

```text
auth    -- OAuth/OIDC/LDAP/session/JWT/API key
iam     -- subject/group/role/grant management
authz   -- matcher/evaluator/middleware/audit
store   -- persistence
```

### 14.2 Rust / Sigbot Core

```text
sigbot_iam::model
sigbot_iam::resolver
sigbot_iam::matcher
sigbot_iam::evaluator
sigbot_iam::audit
sigbot_iam::middleware
```

Shared across languages:

- Resource URN grammar.
- Action identifier naming.
- `iam_action.matchers` JSON schema.
- Grant evaluation algorithm.
- Auth context API contract.
- Golden test fixtures.

## 15. Security principles

1. Default deny.
2. DENY takes precedence over ALLOW.
3. External identities are not directly authorized.
4. Browser JavaScript does not read HttpOnly JWTs.
5. API key plaintext is shown once.
6. API keys can attenuate but not expand owner subject permissions.
7. Secrets do not enter IAM audit metadata.
8. Grants are append-only and are revoked by deletion.
9. Login history goes to audit, not duplicated subject profile fields.
10. UI authorization is experience only; middleware/gateway is the security boundary.
11. Business tables own resource truth; IAM core does not maintain a core resource table.
12. All authorization decisions must be auditable.

## 16. References

- RFC 8141: Uniform Resource Names (URNs): <https://www.rfc-editor.org/rfc/rfc8141.html>
- AWS IAM Amazon Resource Names (ARNs): <https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html>
- GitHub repository roles for an organization: <https://docs.github.com/en/organizations/managing-user-access-to-your-organizations-repositories/managing-repository-roles/repository-roles-for-an-organization>
- Amazon S3 IAM resource types and policy resources: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/security_iam_service-with-iam.html>
