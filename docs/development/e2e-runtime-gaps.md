# Runtime gaps exposed by use-case E2E review

This document tracks acceptance limits found by the real use-case verifiers.
Missing execution evidence must fail validation.

- **Metrics plumbing implemented:** the existing OTel MeterProvider uses the
  official Prometheus reader/exporter, and daemon management listeners expose
  `/metrics` when enabled. JobMaster records actual run transitions and persisted
  attempt results through its existing instruments. Unit tests exercise the
  real exporter and management listener; E2E still requires exact execution-derived
  counters from the deployed Prometheus service.
- **Checkpoint/recovery:** snapshot capture exists, but execution can still
  resolve the latest Agent and Knowledge revisions, and checkpoint restoration
  is inconsistent. Barrier alignment/safepoints, quiescence, in-flight messages,
  snapshot completion, resume and fencing semantics remain under discussion.
  Keep revision/recovery/fencing scenarios out of this change until specified.
- **Committee/human routing:** the manifest supplies Boolean votes while the
  CommitteeExecutor currently counts only objects containing `decision`.
  Both condition branches converge on the human gate. Missing gate execution or
  absent durable approval is a failure, even when other DAG nodes completed.
- **Knowledge:** approval/published hierarchy and text retrieval can be verified.
  A native vector-distance infrastructure probe does not prove that a business
  publication was embedded/retrieved through a configured embedding profile.
- **Deterministic fixture:** its aggregation returns a hard-coded README finding,
  and generated content is not proof of repairing an actual Sonar finding. The
  tightened verifier requires source finding IDs and real re-scan resolution;
  orchestration-only mock output must not count as semantic remediation proof.
- **AutoTest:** missing review agents, Confluence/GitHub resource manifests,
  out-of-map `${item}` references, stdio MCP versus HTTP-only runtime, and
  unimplemented map dispatch prevent the proposed business acceptance graph.
  Preflight must report these before deployment; an empty matrix is not success.
- **Translated allowlist sandbox:** the real Kubernetes main run reaches a
  successful official DeepSeek aggregation, then `git-clone` fails with
  `seccomp notifier handshake failed`. On the same amd64/Rosetta runtime,
  `/proc/1/exe` resolves to Rosetta; directly invoking the real binary's
  `sandbox-child` with a minimal notifier filter returns `install filter:
  invalid argument`. Re-exec and seccomp notification support need separate
  runtime/platform work. Do not disable the manifest's network policy or count
  missing remediation/delivery/rescan evidence as success.

Evidence is scoped by namespace, AgentGraph, Run, NodeRun attempt and plan ID. The UI
scenario must create the workspace file; verification only reads and hashes it.
Trace and log checks must refer to that persisted attempt, not arbitrary events
from another run. Redis/pgvector evidence belongs in scenarios that actually use
those systems; synthetic writes by a verifier are not business execution proof.

Actual local validation on 2026-11-13 also exposed two constraints:

- Official SonarQube `26.4.0.121862-community` (Elasticsearch `8.19.13`) aborts
  under Colima arm64/Rosetta with `seccomp unavailable`. Both Kubernetes and
  Compose reproduce it. [Elasticsearch's bootstrap source](https://github.com/elastic/elasticsearch/blob/v8.19.13/server/src/main/java/org/elasticsearch/bootstrap/Elasticsearch.java)
  requires the execution sandbox unconditionally; disabling bootstrap checks
  does not remove it. The user approved the same official version
  natively on arm64 engines; the shared image policy now selects this exception.
- TaskManager's global `slog` emits text INFO records while its configured
  logger emits JSON. The E2E reader supports both. The DEBUG plan-start event
  still is not emitted by the global logger despite the configured DEBUG level;
  the user approved INFO lifecycle assertions, which are now implemented. Local
  dispatch still lacks distributed SlotWorker success events and remains an
  explicit logging gap.

The approved real-analysis fixture now initializes a fresh Sonar project using
the pinned official SonarSource scanner image and an exact source commit. A real
initial analysis succeeded with 2,075 findings. Re-analysis requires a delivered
commit, a distinct successful CE task and matching analysis revision; exporting
existing findings or accepting an absent task cannot pass. Java dependency
bytecode is not compiled by this fixture, so full type-resolution coverage is
not established.

The approved Colima restart expanded the VM to 14 GiB and restored the previously
running standalone containers. Native Sonar API/admin/MCP checks and official
DeepSeek chat requests from both host and Pod succeeded. The local k3s service
also needed its node/Pod/Service addresses in `NO_PROXY` to make kubelet log
requests bypass the external proxy. No credentials are recorded.

## Standard network isolation direction

The user prefers standard Pod/container network controls over mandatory seccomp
notification for script egress. Use Kubernetes NetworkPolicy with an enforcing
CNI, or Docker internal networks, to isolate execution. Default-deny egress
permits only required DNS/control-plane destinations and a constrained egress
proxy; domain allowlists belong at that proxy. Docker's default bridge is not an
egress restriction, and merely setting HTTP_PROXY does not prevent bypass.

The current Sandbox runs concurrent scripts in one shared network namespace.
Per-plan policies therefore require separate execution Pods/containers or pools
with fixed policy boundaries. Do not mutate one shared Pod's policy while tasks
with different permissions execute. Policy installation/readiness and actual
allow/deny connectivity probes must precede script execution; capability failure
must not silently fall back to unrestricted networking. Seccomp can remain
additional syscall hardening without implementing the business network policy.
This runtime change is recorded separately from the use-case refactor.

Local capability probes on 2026-11-13 passed with amd64/Rosetta Python containers:
Kubernetes NetworkPolicy preserved the selected target and denied another;
Docker internal-only networking preserved an internal target and denied the
previously reachable external-network address. Temporary resources were removed.
These probes establish platform enforcement, not a completed Flowgent backend.

References: [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/),
[Compose internal networks](https://docs.docker.com/reference/compose-file/networks/#internal),
[K3s network policy controller](https://docs.k3s.io/networking/networking-services#network-policy-controller).

The final complete local matrices ran all 25 groups in each backend: Kubernetes
17/25 and Docker 17/25. Both use official DeepSeek and real initial Sonar
analysis, and both stop at the translated allowlist sandbox. The independent
existing-branch scanner rescan succeeds with exact commit/CE/revision correlation;
full remediation/delivery acceptance remains unverified.
