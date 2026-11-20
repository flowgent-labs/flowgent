# Flowgent Kubernetes E2E manual acceptance

This checklist validates the retained `security-autonomy-fixer` environment. The
automated runner must be invoked without `--clean-after-run`; otherwise there is
no environment left to inspect.

## 1. Shell setup and inventory

```bash
cd /home/agent/flowgent
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
export E2E_ROOT="$PWD/use-cases/security-autonomy-fixer/e2e"
k() { sudo -n env KUBECONFIG="$KUBECONFIG" kubectl "$@"; }
h() { sudo -n env KUBECONFIG="$KUBECONFIG" helm "$@"; }

k get namespaces | rg '^e2e-flowgent-'
h list -A | rg 'e2e-flowgent|flowgent'
k get pods -A -o wide | awk 'NR == 1 || $1 ~ /^e2e-flowgent-/'
```

Expected namespaces:

```text
e2e-flowgent-system
e2e-flowgent-authguard-system
e2e-flowgent-mock-github
e2e-flowgent-mock-notification
e2e-flowgent-mock-llm
e2e-flowgent-storage-system
e2e-flowgent-sonarqube-system
e2e-flowgent-workload-security-fixer
```

Every active Pod must be `Running` and Ready; completed one-shot Jobs may be
`Succeeded`. Namespaces provide the E2E prefix, so namespaced resources use
production-style names such as `flowgent-apiserver`, `authguard-authn`,
`envoy-gateway`, `envoy`, `pgvector`, `sonarqube`, and `mock-github`.

Verify that no namespaced workload or Service repeats the environment prefix:

```bash
k get deploy,statefulset,daemonset,service -A -o json |
  jq -r '.items[]
    | select(.metadata.namespace | startswith("e2e-flowgent-"))
    | select(.metadata.name | startswith("e2e-"))
    | [.kind, .metadata.namespace, .metadata.name] | @tsv'
```

The command must print nothing.

## 2. Helm, Gateway, and readiness

```bash
h status flowgent -n e2e-flowgent-system
h status e2e-flowgent-authguard -n e2e-flowgent-authguard-system

k get gateway flowgent-gateway \
  -n e2e-flowgent-authguard-system -o yaml
k get securitypolicy authguard \
  -n e2e-flowgent-authguard-system -o yaml
k get httproute -A

k get deploy,statefulset -A -o custom-columns=\
'NS:.metadata.namespace,KIND:.kind,NAME:.metadata.name,DESIRED:.spec.replicas,READY:.status.readyReplicas' |
  rg '^e2e-flowgent-'
```

The Gateway must be `Accepted=True` and `Programmed=True`; routes and the
AuthGuard SecurityPolicy must be accepted with resolved references.

## 3. Public and diagnostic security boundaries

```bash
curl -sS -o /dev/null -w 'public dashboard: %{http_code}\n' \
  https://flowgent.wl4g.com/dashboard
curl -sS -o /dev/null -w 'public anonymous API: %{http_code}\n' \
  https://flowgent.wl4g.com/api/v1/security-fixer/flows

NODE_IP=$(k get node -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
curl -sS -o /dev/null -w 'NodePort dashboard: %{http_code}\n' \
  "http://${NODE_IP}:31080/dashboard"
curl -sS -o /dev/null -w 'NodePort anonymous API: %{http_code}\n' \
  "http://${NODE_IP}:31080/api/v1/security-fixer/flows"

curl -sS https://flowgent.wl4g.com/.well-known/authn.json |
  jq '{application, standalone}'
```

Expected results:

- Both dashboard requests return the SPA shell with HTTP `200`.
- Both anonymous API requests return `401`; NodePort must not bypass AuthGuard.
- AuthN metadata reports password login enabled and TOTP disabled.
- A fresh browser visiting the public dashboard is redirected to Hosted Login
  before any protected business data is rendered.

## 4. Acceptance credentials

The retained password is generated for each clean deployment and is stored only
in the E2E Secret:

```bash
k get secret acceptance-credentials \
  -n e2e-flowgent-authguard-system \
  -o go-template='username={{index .data "username" | base64decode}}{{"\n"}}password={{index .data "password" | base64decode}}{{"\n"}}'
```

Open <https://flowgent.wl4g.com/dashboard>, sign in with those credentials, and
leave the TOTP field empty—the retained acceptance profile intentionally has
TOTP disabled.

## 5. UI acceptance

Use the left navigation and complete these checks:

1. **Overview** — verify 24-hour, 7-day, and 30-day run totals, duration, and
   success/failure curves render without an error banner.
2. **Runs** — open a completed `security-autonomy-fixer` run. The DAG must show
   live/final node state, attempts, retries, timestamps, and logs.
3. Select `generate-fixes` — the right panel must show a human-readable file
   change preview (file tabs and added/removed lines), with raw JSON secondary.
4. Click **Focus this attempt in trace** — Tracking must open with the exact
   `node_run_id` attempt selected, scrolled into view, and highlighted in the
   waterfall/span detail.
5. **Flows** — open the visual DAG editor, inspect its embedded recent-runs list,
   save a harmless description change, and verify the revision increments.
6. **Agents** — inspect soul, instruction, input/output schemas, and immutable
   revision history. A save must create a new revision.
7. **Skills** — inspect instructions plus `assets/` and `scripts/`; upload an
   allow-listed text/image/PDF/XLS asset or helper script and verify a new
   revision and hash-backed file entry.
8. **MCPs** — inspect GitHub and SonarQube RPC URLs and environment references;
   secrets must not be displayed in clear text.
9. **LLMs** — inspect the deterministic E2E provider and retained DeepSeek
   provider. Type choices must be limited to OpenAI, Anthropic, or Gemini.
10. **Knowledge** — verify published content is read-only. Publication must go
    through candidate → unified approval → immutable revision rather than a
    direct create/edit control.
11. **Notifications** — inspect the imported encrypted channels and exercise a
    non-sensitive test update if desired.
12. Open the top-right account menu — it must show the Flowgent/AuthGuard custom
    name and logo. **Account Security** must open in the same-page modal without
    a full navigation refresh.
13. Sign out, then sign back in with the retained password. The canonical cookie
    must be cleared on logout and recreated after Hosted Login.

## 6. Data, vector, and observability probes

```bash
k exec -n e2e-flowgent-storage-system pgvector-0 -- \
  psql -U test -d flowgent -Atc \
  "SELECT extversion FROM pg_extension WHERE extname='vector';
   SELECT '[1,0,0]'::vector <-> '[0,1,0]'::vector;"

k exec -n e2e-flowgent-storage-system pgvector-0 -- \
  psql -U test -d flowgent -P pager=off -c \
  "SET search_path TO e2e_flowgent;
   SELECT status, count(*) FROM orh_run GROUP BY status ORDER BY status;"

k port-forward -n e2e-flowgent-system service/flowgent-jaeger 16688:16686
# In another terminal: open http://127.0.0.1:16688

curl -sS http://127.0.0.1:32000/api/system/status | jq
```

The pgvector extension must be present and the vector distance query must
return a numeric value. Jaeger must contain traces linked to the retained Runs.
SonarQube must report `UP`.

## 7. Automated evidence

```bash
sed -n '1,260p' "$E2E_ROOT/reports/00_summary.md"
find "$E2E_ROOT/reports/round-01/evidence" -type f | sort
```

The full matrix must report `23/23 passed`. Important evidence includes the UI
Flow/Run/Trace report (`41`), AuthGuard/login report (`46`), Jaeger coverage
report (`13`), and Controller/JM/TM/Sandbox lifecycle report (`23`).

## 8. Cleanup and post-cleanup audit

Run this only after human acceptance is finished:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
sudo -n -E env KUBECONFIG="$KUBECONFIG" \
  "$E2E_ROOT/.venv/bin/python" "$E2E_ROOT/runner.py" --deployer k8s --cleanup

k get namespaces -o name | rg '^namespace/e2e-flowgent-' || true
h list -A -o json | jq -r '.[] | select(.namespace | startswith("e2e-flowgent-"))'
k get pods -A -o json |
  jq -r '.items[] | select(.metadata.namespace | startswith("e2e-flowgent-")) | .metadata.name'
k get gatewayclass,clusterrole,clusterrolebinding,mutatingwebhookconfiguration -o name |
  rg 'e2e-flowgent' || true
```

Cleanup first performs normal Helm uninstall and namespace deletion. Only after
a bounded timeout does it remove finalizers from residual objects, force-delete
remaining Pods, use the Kubernetes namespace `/finalize` subresource, and verify
that releases, namespaces, Pods, PVs, and E2E-owned cluster resources are gone.
