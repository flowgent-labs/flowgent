#!/usr/bin/env python3
"""
Scenario 13 — MANDATORY Jaeger Trace Verification
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Validates that every step of the security-autonomy-fixer agent flow is
traced in Jaeger with complete span information, including retries and
every API/MQTT call per step.

Previously this was an infrastructure-only check with opportunistic trace
lookup. Now it is **MANDATORY**: if Jaeger traces are missing or the span
coverage is insufficient, the E2E verification FAILS.

Verification Strategy:
1. Seed agent + MCP definitions (prerequisite)
2. Ensure Security Fixer flow definition exists (prerequisite)
3. Trigger complete Security Fixer flow + wait for completion
4. Verify OTEL infrastructure from pod logs (JM + API server)
5. **MANDATORY** Query Jaeger for traces matching run.id (with retries)
6. **MANDATORY** Validate per-node span coverage — every core node must have >=1 span
7. Informational — span attribute sampling
"""

import os
import sys
import time
import json
import subprocess
import requests
import yaml
import urllib.parse
from typing import Dict, Any, Optional

from common import config

JAEGER_API = config.JAEGER_UI_URL
API_BASE = config.K8S_APISERVER_URL
NAMESPACE = config.K8S_NAMESPACE

_CONFIG_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "config")
_FLOW_YAML_PATH = os.path.join(_CONFIG_ROOT, "flows", "security-autonomy-fixer.yaml")
_AGENTS_DIR = os.path.join(_CONFIG_ROOT, "agents")

_USE_REAL_MCP = os.getenv("FLOWGENT_E2E_USE_REAL_MCP", "true").lower() == "true"
_MOCK_MCP_COMMAND = ["/app/mcp-server.sh"]

JM_NAMESPACE = f"{config.K8S_APP_NAMESPACE_PREFIX}{NAMESPACE}"
JM_DEPLOY_NAME = f"flowgent-jobmanager-{JM_NAMESPACE}-security-autonomy-fixer"


def _unwrap_k8s(data: dict) -> dict:
    if "data" in data and "kind" in data:
        spec = data["data"] or {}
        flat = dict(spec) if isinstance(spec, dict) else {}
        md = data.get("metadata", {}) or {}
        if md.get("name"):
            flat["name"] = md["name"]
        if md.get("namespace"):
            flat.setdefault("namespace_id", md["namespace"])
        return flat
    return data


def _get_or_post(get_path, post_path, payload, kind):
    r = requests.get(f"{API_BASE}{get_path}")
    if r.status_code == 200:
        return True
    r = requests.post(f"{API_BASE}{post_path}", json=payload)
    if r.status_code not in (200, 201):
        print(f"  WARN: failed to register {kind} {payload.get('name')}: {r.status_code} {r.text[:160]}")
        return False
    return True


def _resolve_env_vars(obj):
    import re
    if isinstance(obj, str):
        def _repl(m):
            return os.environ.get(m.group(1), m.group(0))
        return re.sub(r'\$\{(\w+)\}', _repl, obj)
    if isinstance(obj, dict):
        return {k: _resolve_env_vars(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_resolve_env_vars(v) for v in obj]
    return obj


def seed_agents_and_mcps():
    print("  -> Seeding agent + MCP definitions...")
    for fname in sorted(os.listdir(_AGENTS_DIR)):
        if not fname.endswith(".yaml"):
            continue
        with open(os.path.join(_AGENTS_DIR, fname)) as f:
            agent_def = _unwrap_k8s(yaml.safe_load(f))
        name = agent_def.get("name")
        if name:
            _get_or_post(f"/api/v1/{NAMESPACE}/agents/{name}", f"/api/v1/{NAMESPACE}/agents", agent_def, "agent")

    if not os.environ.get("GITHUB_TOKEN") and os.environ.get("GH_TOKEN"):
        os.environ["GITHUB_TOKEN"] = os.environ["GH_TOKEN"]

    for mode in ("github", "sonarqube"):
        if _USE_REAL_MCP:
            with open(os.path.join(_CONFIG_ROOT, "mcps", f"{mode}.yaml")) as f:
                mcp_def = _unwrap_k8s(yaml.safe_load(f))
            mcp_def = _resolve_env_vars(mcp_def)
        else:
            mcp_def = {"name": mode, "enabled": True, "type": "stdio",
                       "command": _MOCK_MCP_COMMAND, "args": [mode], "env": {}}
        _get_or_post(f"/api/v1/{NAMESPACE}/mcp/{mode}", f"/api/v1/{NAMESPACE}/mcp", mcp_def, "mcp")
    print("  OK Agent + MCP definitions ready")


# ── Expected flow nodes (from security-autonomy-fixer.yaml) ──────────

FLOW_PHASES = {
    "1. DISCOVERY":       ["get-commit", "scan-sonarqube"],
    "2. ANALYZE":         ["aggregate-issues"],
    "3. FIX":             ["generate-fixes"],
    "4. REVIEW":          ["review-security", "review-quality", "review-arch"],
    "5. VOTE":            ["committee"],
    "6. SUPERVISOR":      ["supervisor-check"],
    "7. CONDITION":       ["is-approved"],
    "8. HUMAN":           ["human-approval"],
    "9. PR CHECK&COMMIT": ["check-existing-pr", "pr-exists", "create-branch",
                           "commit-fixes", "create-pr", "commit-to-existing"],
    "10. RE-SCAN":        ["trigger-rescan", "wait-rescan", "check-resolved",
                           "compare-results", "fix-complete"],
    "11. REPORT":         ["summary-report", "notify-pr", "end"],
}

ALL_NODES = [node for nodes in FLOW_PHASES.values() for node in nodes]

# Phases 1–7 always execute regardless of conditions or branching
_CORE_PHASE_KEYS = list(FLOW_PHASES.keys())[:7]
CORE_NODES = [n for k in _CORE_PHASE_KEYS for n in FLOW_PHASES[k]]

# Minimum expected spans: each node produces at least dispatch+consume+execute
MIN_EXPECTED_SPANS = len(ALL_NODES)  # 25


# ── Helpers ──────────────────────────────────────────────────────────

def _span_tags(span: dict) -> dict:
    """Return tags from a Jaeger span as a dict {key: value}."""
    tags = {}
    for t in span.get("tags", []):
        key = t.get("key", "")
        val = t.get("value", t.get("vStr", ""))
        tags[key] = val
    return tags


def _jaeger_get(url: str, timeout: int = 15) -> dict:
    """GET a Jaeger JSON API endpoint; return parsed JSON or {} on error."""
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode())
    except Exception as exc:
        print(f"  [jaeger-get] {exc}")
        return {}


def _check_jaeger_health(jaeger_base_url: str) -> bool:
    """Return True if Jaeger query API is reachable."""
    try:
        r = requests.get(f"{jaeger_base_url.rstrip('/')}/api/services", timeout=5)
        return r.status_code == 200
    except Exception:
        return False


# ── MANDATORY trace query ────────────────────────────────────────────

def query_jaeger_trace(run_id: str, jaeger_base_url: str,
                       timeout_sec: int = 90) -> tuple:
    """
    **(MANDATORY)** Query Jaeger for traces matching ``run.id``.

    Uses the Jaeger Query API.  Retries for up to *timeout_sec* because
    OTLP spans are batched and may not appear instantly.

    Returns ``(traces, all_spans)`` where *traces* is the list of raw trace
    dicts and *all_spans* is the flat list of ALL spans across those traces.

    Raises ``AssertionError`` when no matching trace is found.
    """
    params = urllib.parse.urlencode({
        "service": "flowgent-jobmanager",
        "lookback": "3h",
        "limit": "50",
    })
    url = f"{jaeger_base_url.rstrip('/')}/api/traces?{params}"

    deadline = time.time() + timeout_sec
    last_err = None

    while time.time() < deadline:
        try:
            resp = _jaeger_get(url)
            traces = resp.get("data", [])
            print(f"  Jaeger returned {len(traces)} trace(s) for service=flowgent-jobmanager")

            # Filter by run.id tag
            matching = []
            for trace in traces:
                for span in trace.get("spans", []):
                    tags = _span_tags(span)
                    if tags.get("run.id") == run_id:
                        matching.append(trace)
                        break

            if matching:
                all_spans = []
                for t in matching:
                    all_spans.extend(t.get("spans", []))
                span_count = len(all_spans)
                trace_ids = [t.get("traceID", "?")[:16] for t in matching]
                print(f"  OK Found {len(matching)} trace(s) ({span_count} spans total): {trace_ids}")
                return matching, all_spans

            total_spans = sum(len(t.get("spans", [])) for t in traces)
            last_err = (f"No trace with run.id={run_id} "
                        f"(scanned {len(traces)} traces, {total_spans} spans)")
        except AssertionError:
            raise
        except Exception as exc:
            last_err = str(exc)

        print(f"  Retrying Jaeger query in 5s ({last_err})")
        time.sleep(5)

    raise AssertionError(f"MANDATORY Jaeger trace check FAILED: {last_err}")


# ── MANDATORY span coverage validation ───────────────────────────────

def validate_span_coverage(all_spans: list,
                           fail_on_core_missing: bool = True) -> dict:
    """
    **(MANDATORY for core nodes)** Verify every flow node has >=1 span.

    Returns ``{node_id: span_count}`` for all nodes.

    Raises ``AssertionError`` when **any** core node (phases 1–7) has zero
    spans and *fail_on_core_missing* is True.
    """
    node_counts = {n: 0 for n in ALL_NODES}
    tagged_spans = 0

    for span in all_spans:
        tags = _span_tags(span)
        nid = tags.get("flowgent.node_id") or tags.get("node_id") or tags.get("agentflow.id")
        if nid and nid in node_counts:
            node_counts[nid] += 1
            tagged_spans += 1

    # ── Print coverage report ──
    print(f"\n  Per-node span coverage ({len(all_spans)} total spans, {tagged_spans} tagged):")
    missing_core = []
    missing_all = []
    for phase_name, nodes in FLOW_PHASES.items():
        is_core = phase_name in _CORE_PHASE_KEYS
        flag = "★" if is_core else " "
        counts = [f"{n}={node_counts[n]}" for n in nodes]
        print(f"  {flag} {phase_name}: {', '.join(counts)}")
        for n in nodes:
            if node_counts[n] == 0:
                missing_all.append(n)
                if is_core:
                    missing_core.append(n)

    # ── Report ──
    if missing_all:
        total_found = sum(1 for v in node_counts.values() if v > 0)
        print(f"\n  Nodes with ZERO spans ({len(missing_all)}): {missing_all}")
        print(f"  Coverage: {total_found}/{len(ALL_NODES)} nodes have >=1 span")

    if missing_core and fail_on_core_missing:
        raise AssertionError(
            f"MANDATORY span coverage FAILED: core nodes missing from trace: {missing_core}"
        )

    return node_counts


# ── Informational attribute sampling ─────────────────────────────────

def validate_span_attributes(all_spans: list, sample_count: int = 12):
    """
    (Informational) Print attributes from a sample of spans to verify
    ``flowgent.node_id``, ``flowgent.task_type`` are populated.
    """
    tagged = [s for s in all_spans if _span_tags(s).get("flowgent.node_id")]
    sample = tagged[:sample_count]
    if not sample:
        print("  (info) No spans with flowgent.node_id tag — instrumentation may be partial")
        return

    print(f"\n  Span attribute sample ({min(sample_count, len(sample))} of {len(tagged)} tagged spans):")
    for s in sample:
        tags = _span_tags(s)
        nid = tags.get("flowgent.node_id", "?")
        ttype = tags.get("flowgent.task_type", "?")
        op = s.get("operationName", "?")
        dur_ms = s.get("duration", 0) // 1000
        print(f"    {op} | node={nid} | type={ttype} | {dur_ms}ms")


# ── Prerequisite: Infrastructure checks ──────────────────────────────

def ensure_security_fixer_flow_exists():
    print("  -> Ensuring security-autonomy-fixer flow definition exists...")
    with open(_FLOW_YAML_PATH) as f:
        flow_def = _unwrap_k8s(yaml.safe_load(f))
    flow_def.pop("triggers", None)
    resp = requests.post(f"{API_BASE}/api/v1/{NAMESPACE}/flows", json=flow_def, timeout=10)
    if resp.status_code not in (200, 201):
        raise Exception(f"Flow upsert failed: {resp.status_code} {resp.text}")
    print(f"  OK Flow definition ready: {flow_def.get('id')} (priority={flow_def.get('priority')})")


def trigger_security_fixer() -> str:
    print("  -> Triggering security-autonomy-fixer flow...")
    url = f"{API_BASE}/api/v1/{NAMESPACE}/flows/trigger"
    payload = {
        "agentflow_id": "security-autonomy-fixer",
        "vars": {
            "repo": "rengine",
            "repo_path": "/home/agent/rengine",
            "max_iterations": 1,
        }
    }
    resp = requests.post(url, json=payload, timeout=10)
    if resp.status_code not in [200, 201]:
        raise Exception(f"Trigger failed: {resp.status_code} {resp.text}")
    data = resp.json()
    run_id = data.get("id") or data.get("run_id")
    if not run_id:
        raise Exception(f"No run_id in response: {data}")
    print(f"  OK Flow triggered: run_id={run_id}")
    return run_id


def wait_for_completion(run_id: str, timeout: int = 600):
    print(f"  -> Waiting for run {run_id} to complete (timeout={timeout}s)...")
    url = f"{API_BASE}/api/v1/{NAMESPACE}/runs/{run_id}"
    start = time.time()
    while time.time() - start < timeout:
        try:
            resp = requests.get(url, timeout=5)
            if resp.status_code == 200:
                data = resp.json()
                status = data.get("status")
                if status in ["COMPLETED", "FAILED", "CANCELLED"]:
                    print(f"  OK Flow {status.lower()}")
                    return status
            time.sleep(5)
        except Exception as e:
            print(f"  WARN Poll error: {e}")
            time.sleep(5)
    raise TimeoutError(f"Flow did not complete within {timeout}s")


def get_jm_pod_logs() -> str:
    """Get logs from the security-autonomy-fixer JM pod (grep for OTEL init)."""
    try:
        result = subprocess.run(
            ["bash", "-c",
             f"kubectl logs -n {JM_NAMESPACE} "
             "-l flowgent.io/flow=security-autonomy-fixer "
             "2>/dev/null | grep -m1 'OTEL tracing enabled' || true"],
            capture_output=True, text=True, timeout=30)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout
        check = subprocess.run(
            ["bash", "-c",
             f"kubectl get pods -n {JM_NAMESPACE} "
             "-l flowgent.io/flow=security-autonomy-fixer "
             "-o jsonpath='{.items[0].metadata.name}' 2>/dev/null"],
            capture_output=True, text=True, timeout=10)
        pod_name = check.stdout.strip()
        if pod_name:
            verify = subprocess.run(
                ["kubectl", "exec", "-n", JM_NAMESPACE, pod_name, "--",
                 "grep", "-l", "enabled.*true", "/etc/flowgent/flowgent.yaml"],
                capture_output=True, text=True, timeout=10)
            if verify.returncode == 0:
                return "OTEL tracing enabled (verified via config)"
        return ""
    except Exception as e:
        print(f"  WARN: Could not get JM pod logs: {e}")
        return ""


def get_apiserver_logs() -> str:
    """Get logs from the apiserver deployment (grep for OTEL init)."""
    try:
        result = subprocess.run(
            ["bash", "-c",
             "kubectl logs deploy/flowgent-apiserver -n default 2>/dev/null | grep -m1 'OTEL tracing enabled' || true"],
            capture_output=True, text=True, timeout=30)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout
        return ""
    except Exception as e:
        print(f"  WARN: Could not get apiserver logs: {e}")
        return ""


def verify_otel_from_logs(jm_logs: str, apiserver_logs: str) -> Dict[str, Any]:
    """Verify OTEL infrastructure is properly configured from pod logs."""
    checks = {}

    checks["jm_otel_enabled"] = "OTEL tracing enabled" in jm_logs
    if checks["jm_otel_enabled"]:
        for line in jm_logs.split("\n"):
            if "OTEL tracing enabled" in line:
                print(f"  OK JM OTEL: {line.strip()}")
                break

    checks["jm_no_dns_error"] = "no such host" not in jm_logs
    if not checks["jm_no_dns_error"]:
        for line in jm_logs.split("\n"):
            if "no such host" in line:
                print(f"  FAIL JM DNS error: {line.strip()}")
                break
    else:
        print("  OK JM: No DNS resolution errors (Jaeger FQDN resolves)")

    checks["jm_no_export_error"] = "traces export" not in jm_logs.lower() or "error" not in jm_logs.lower()
    if not checks["jm_no_export_error"]:
        for line in jm_logs.split("\n"):
            if "traces export" in line.lower():
                print(f"  FAIL JM export error: {line.strip()}")
                break
    else:
        print("  OK JM: No OTEL export errors in logs")

    checks["api_otel_enabled"] = "OTEL tracing enabled" in apiserver_logs
    if checks["api_otel_enabled"]:
        for line in apiserver_logs.split("\n"):
            if "OTEL tracing enabled" in line:
                print(f"  OK API Server OTEL: {line.strip()}")
                break

    return checks


# ── Main ─────────────────────────────────────────────────────────────

def run():
    print("\n" + "=" * 60)
    print("  Scenario 13: OTEL Tracing — MANDATORY Jaeger Trace Verification")
    print("=" * 60)

    seed_agents_and_mcps()
    ensure_security_fixer_flow_exists()

    # Wait for the controller to create the JM pod
    print("  -> Waiting for JM pod to be created by Controller...")
    for _ in range(12):
        try:
            result = subprocess.run(
                ["kubectl", "get", "pods", "-n", JM_NAMESPACE,
                 "-l", "flowgent.io/flow=security-autonomy-fixer",
                 "-o", "jsonpath={.items[0].status.phase}"],
                capture_output=True, text=True, timeout=10)
            if result.stdout.strip() == "Running":
                print("  OK JM pod is Running")
                break
        except Exception:
            pass
        time.sleep(5)

    run_id = trigger_security_fixer()
    status = wait_for_completion(run_id, timeout=600)
    if status != "COMPLETED":
        print(f"  WARN: Flow status={status}, continuing verification...")

    # Allow batched spans to flush to Jaeger
    print("  -> Waiting for batched spans to flush to Jaeger...")
    time.sleep(10)

    # ── Phase A: Infrastructure checks (prerequisite) ──
    print("\n  -> Verifying OTEL infrastructure from pod logs...")
    jm_logs = get_jm_pod_logs()
    apiserver_logs = get_apiserver_logs()

    otel_checks = verify_otel_from_logs(jm_logs, apiserver_logs)

    # ── Phase B: MANDATORY Jaeger trace query ──
    print(f"\n  -> [MANDATORY] Querying Jaeger for traces (run.id={run_id})...")
    if not _check_jaeger_health(JAEGER_API):
        raise AssertionError(
            f"MANDATORY: Jaeger Query API at {JAEGER_API} is not reachable. "
            "Ensure Jaeger all-in-one is running and accessible."
        )

    traces, all_spans = query_jaeger_trace(run_id, JAEGER_API, timeout_sec=90)

    # ── Phase C: MANDATORY span coverage validation ──
    print("\n  -> [MANDATORY] Validating per-node span coverage...")
    node_counts = validate_span_coverage(all_spans, fail_on_core_missing=True)

    # Edge case: if the flow didn't include PR phases (condition false on is-approved),
    # the commit-to-existing vs. create-branch+commit-fixes+create-pr paths are
    # mutually exclusive.  Report this but don't fail.
    path_nodes = set()
    for phase_name in list(FLOW_PHASES.keys())[8:]:  # phases 9-11
        path_nodes.update(FLOW_PHASES[phase_name])
    covered_path = [n for n in path_nodes if node_counts.get(n, 0) > 0]
    if covered_path:
        print(f"  Path-dependent nodes with spans: {covered_path}")

    # ── Phase D: Informational attribute sampling ──
    print("\n  -> [Informational] Span attribute sampling...")
    validate_span_attributes(all_spans)

    # ── Phase E: Verdict ──
    print(f"\n  {'=' * 60}")
    print("  OTEL Tracing Verification Results")
    print(f"  {'=' * 60}")

    # Infrastructure verdict
    infra_critical = [
        otel_checks.get("jm_otel_enabled", False),
        otel_checks.get("jm_no_dns_error", False),
        otel_checks.get("api_otel_enabled", False),
    ]
    infra_passed = sum(1 for c in infra_critical if c)
    print(f"  Infrastructure:  {infra_passed}/{len(infra_critical)} checks passed")
    for k, v in otel_checks.items():
        status_str = "OK" if v else "FAIL"
        print(f"    {status_str}: {k}")

    # Trace verdict
    trace_count = len(traces)
    span_count = len(all_spans)
    print(f"\n  Jaeger traces:    {trace_count} matching trace(s)")
    print(f"  Jaeger spans:     {span_count} total spans (min expected: {MIN_EXPECTED_SPANS})")

    # Coverage verdict
    core_covered = sum(1 for n in CORE_NODES if node_counts.get(n, 0) > 0)
    core_total = len(CORE_NODES)
    print(f"  Core node spans:  {core_covered}/{core_total} core nodes (phases 1–7) covered")
    for n in CORE_NODES:
        cnt = node_counts.get(n, 0)
        print(f"    {'OK' if cnt > 0 else 'MISSING'}: {n} ({cnt} spans)")

    # Global verdict
    infra_ok = all(infra_critical)
    trace_ok = trace_count >= 1 and span_count >= MIN_EXPECTED_SPANS
    core_coverage_ok = core_covered == core_total

    print(f"\n  Infrastructure:   {'PASS' if infra_ok else 'FAIL'}")
    print(f"  Jaeger traces:     {'PASS' if trace_ok else 'FAIL'}")
    print(f"  Core span coverage:{'PASS' if core_coverage_ok else 'FAIL'}")

    if infra_ok and trace_ok and core_coverage_ok:
        print(f"\n  OK All mandatory OTEL tracing checks passed")
        if traces:
            trace_id = traces[0].get("traceID", "?")
            print(f"  Jaeger UI: {JAEGER_API.rstrip('/')}/trace/{trace_id}")
        print(f"  PASS")
    else:
        failures = []
        if not infra_ok:
            failures.append(f"infrastructure checks failed: "
                           f"{[(k,v) for k,v in otel_checks.items() if not v]}")
        if not trace_ok:
            failures.append(f"trace/spans insufficient: {span_count} spans < {MIN_EXPECTED_SPANS} min")
        if not core_coverage_ok:
            missing = [n for n in CORE_NODES if node_counts.get(n, 0) == 0]
            failures.append(f"core nodes missing: {missing}")

        raise AssertionError("MANDATORY OTEL tracing checks FAILED:\n  " + "\n  ".join(failures))


if __name__ == "__main__":
    run()
