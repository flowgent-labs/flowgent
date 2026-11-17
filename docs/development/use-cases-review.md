# Use-case structure and scenario review (draft)

**Status:** Interim review notes and historical validation observations; not an
architecture specification or current release acceptance report.

[中文草稿](use-cases-review_ZH.md)

Each case owns `runner.py`, `deploy/flowgent/config/`, `deploy/`, `util/`, `verifier/` and
`reports/`. Deployment assets are grouped horizontally by service; verification
is grouped vertically by the operation that produces the evidence. PG/vector,
workspace, traces and logs stay with that operation rather than separate storage
scenarios.

Architecture references: [overview](../architecture/overview.md),
[data lifecycle](../architecture/data-lifecycle.md),
[AgentFlow](../architecture/agent-flow.md), and the case READMEs.

## Confirmed scenario decisions

| Item | Decision and resulting contract |
| --- | --- |
| 1. Security observability | Approved. Move s13 to `otel/`; add s51 metrics and s52 logs after business execution. Use 100% trace sampling and exact Run/NodeRun attempt/Plan identities. Prometheus must verify actual business counter/histogram changes. |
| 2. Security assertions | Approved. s32–35 fail missing evidence on the active branch. Source finding IDs, generated files, delivery bytes, persisted approvals and actual rescan results are required. |
| 3. Revision/recovery/fencing/denial | Deferred. Checkpoint/barrier/safepoint semantics are under discussion; record implementation gaps without adding passing placeholders or modifying the Go engine. |
| 4. AutoTest baseline | Approved. Add resource/data-scope preflight, infra and OTEL groups. Missing dependencies and an empty matrix cannot pass. |
| 5. AutoTest business scenarios | Deferred until missing agents/MCPs and map/join/feedback contracts are agreed. Do not invent mocks or acceptance results for these paths. |
| 6. Log lifecycle | Approved. Require actual INFO sandbox trigger/result and TaskManager success events, correlated to the producing Plan/Run, instead of the unavailable DEBUG start event. Parse both JSON and Go text logs. |
| 7. Real scan and rescan | Approved. Use the pinned official SonarSource scanner for initial analysis. Rescan must use the delivered commit and a distinct successful CE task with a matching analysis revision. An existing findings export or absent task cannot pass. |

Security retains the original 23 IDs and adds telemetry IDs 51/52 (25 groups):

| IDs | Vertical coverage |
| --- | --- |
| 01, 11, 12, 14 | Layout, infrastructure, console import, authentication |
| 21–25 | API lifecycle, notification delivery, runtime lifecycle, MQTT dispatch, A2A |
| 31–37 | Seed/scan/trigger, discovery, remediation, delivery/rescan, commits, knowledge, workspace |
| 41–46 | Browser flow execution, agent, skill, integrations, knowledge/notifications, Hosted Login |
| 13, 51, 52 | Persisted business execution → tracing, metrics and lifecycle logs |

The UI Flow writes its own workspace file; the verifier reads its bytes/hash and
correlates the same persisted attempt with Jaeger and runtime logs. Main-flow
checks retain each active node's identity. Database probes written by a verifier
are never presented as business-produced persistence or embedding evidence.

## Structure and runtime policy

- Both former `e2e/` trees have moved to their case roots. Every scenario file
  defines its named class and `deploy/init/verifier` lifecycle. Generic helpers
  are implemented once in `tests/e2e_util/` and exposed through the permitted
  case `util/` modules. Application clients belong to `verifier/context.py`.
- `deploy/flowgent/` contains only configuration, its Compose override and links
  to formal root Dockerfiles/ignore files. Helm is loaded directly from
  `deploy/helm/flowgent`; no case-owned Helm copy or link is permitted. Case deployment fixtures live
  under `verifier/infra/`. Other components own source, Dockerfile, Compose and
  Helm assets; source-level test directories are absent.
- AutoTest's legacy Go MCPs keep their actual stdio contract with optional
  `manual-stdio` profiles/attachable Pods. Both compile and answer MCP
  initialize/tools-list. Their HTTP integration remains deferred under item 5.
- One image matrix supplies acquisition references, aliases, build arguments,
  Compose and Helm. Region priority is `IN_GFW`, then `NON_GFW`, then legacy
  `IN_CN_GFW`; explicit overrides use `FLOWGENT_E2E_IMAGE_<KEY>`. Replacement
  matches complete references rather than prefixes of unrelated tags.
- Workload images use linux/amd64, including translated macOS/Windows engines.
  macOS Solana uses the prescribed `3.1.14-arm64` artifact. The user approved
  same-version official native-arm64 SonarQube because Elasticsearch seccomp
  cannot start under Rosetta. Scanner/MCP/PostgreSQL remain amd64.
- Colima, OrbStack and native Docker/k3s/containerd paths share runtime discovery
  and architecture checks. Windows process cleanup uses taskkill. Actual
  OrbStack/Windows machines were unavailable; those paths have contract tests.
- Image acquisition uses `~/mirror-pull.sh` and the existing Aliyun registry.
  No intermediate fixture image is needed for the official scanner: its Python
  adapter is mounted into the pinned vendor image. Source downloads are cached
  by exact commit, and scanner jobs never fall back to a latest task.

## Validation and remaining limits

Builds, real matrices and their exact artifact overrides are recorded in the
case reports. Framework checks cover both region renderings, component assets,
image/runtime/platform selection, lifecycle/class rules, missing-evidence
failures, scoped collector RBAC and process cleanup.

The approved 14-GiB Colima restart completed, and previously running standalone
containers were restored. Official DeepSeek chat requests succeeded from the
host and a Pod. Native Sonar API/admin checks and its 385-tool MCP bridge passed;
real initial analysis produced 2,075 findings. These checks alone do not prove
end-to-end remediation or delivery.

AutoTest's complete default entry point was also executed: its preflight fails
before mutation on missing agents/MCPs, out-of-map item references and the
unsupported map/feedback contract. No complete AutoTest application pass is
claimed. The shared Go runtime limitations remain in the compact
[gap list](e2e-runtime-gaps.md).

Final local matrices executed all 25 groups for both backends: Kubernetes
**17/25**, Docker **17/25**, using official DeepSeek and real Sonar analysis.
Both main runs stop at the translated seccomp allowlist sandbox. Metrics and
Docker local-dispatch log gaps also fail their strict assertions. The scanner's
real existing-branch rescan and both standard network capability probes pass
independently. Root builds and 42 shared E2E checks pass. Exact evidence and
artifact overrides are in the [local validation report](../../use-cases/security-autonomy-fixer/reports/VALIDATION.md).
