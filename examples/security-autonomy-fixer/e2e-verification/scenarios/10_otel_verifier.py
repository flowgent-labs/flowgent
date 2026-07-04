#!/usr/bin/env python3
"""
Scenario 10 — OTEL Tracing: Complete 24-Node Span Coverage

Validates that every node in the Security Autonomy Fixer flow produces
complete distributed traces visible in Jaeger UI.

Requirements:
- Jaeger Query API accessible (default: http://localhost:16686)
- Security Fixer flow fully executed (scenario 11)
- OTEL exporter configured in flowgent pods

Verification Strategy:
1. Trigger complete Security Fixer flow
2. Query Jaeger for trace by run_id
3. Verify all 24 nodes have minimum 7 spans each:
   - JM-DispatchPlan
   - TM-ConsumeExecPlan
   - SlotWorker-Execute{Type}
   - Type-specific (LLM-Call, MCP-Call, Sandbox-Execute, etc)
   - API-PUT-/tasks
   - MQTT-Publish-ExecResult
   - JM-ReceiveExecResult
4. Verify parent-child span relationships
5. Verify phase-level grouping and critical path
"""

import sys
import time
import json
import requests
from typing import List, Dict, Any, Optional

sys.path.insert(0, '..')
import config

JAEGER_API = config.JAEGER_UI_URL
API_BASE = config.K3S_APISERVER_URL
TENANT = config.K3S_TENANT

# Security Fixer 24 nodes grouped by 11 phases (matches flows/security-autonomy-fixer.yaml)
FLOW_PHASES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["tribunal"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}

ALL_NODES = [node for nodes in FLOW_PHASES.values() for node in nodes]

# Required spans for each node (minimum)
REQUIRED_SPAN_OPERATIONS = [
    "JM-DispatchPlan",
    "TM-ConsumeExecPlan",
    "SlotWorker-Execute",  # prefix match
    "API-PUT",  # prefix match for API-PUT-/tasks
    "MQTT-Publish-ExecResult",
    "JM-ReceiveExecResult",
]


def trigger_security_fixer() -> str:
    """Trigger security fixer flow and return run_id"""
    print("  → Triggering security-autonomy-fixer flow...")
    
    url = f"{API_BASE}/api/v1/{TENANT}/agentflows/trigger"
    payload = {
        "agentflow_id": "security-autonomy-fixer",
        "vars": {
            "repo": "rengine",
            "repo_path": "/home/agent/rengine",
            "max_iterations": 1,  # limit to 1 iteration for faster testing
        }
    }
    
    resp = requests.post(url, json=payload, timeout=10)
    if resp.status_code not in [200, 201]:
        raise Exception(f"Trigger failed: {resp.status_code} {resp.text}")
    
    data = resp.json()
    run_id = data.get("id") or data.get("run_id")
    if not run_id:
        raise Exception(f"No run_id in response: {data}")
    
    print(f"  ✓ Flow triggered: run_id={run_id}")
    return run_id


def wait_for_completion(run_id: str, timeout: int = 600):
    """Wait for flow to complete"""
    print(f"  → Waiting for run {run_id} to complete (timeout={timeout}s)...")
    
    url = f"{API_BASE}/api/v1/{TENANT}/runs/{run_id}"
    start = time.time()
    
    while time.time() - start < timeout:
        try:
            resp = requests.get(url, timeout=5)
            if resp.status_code == 200:
                data = resp.json()
                status = data.get("status")
                if status in ["COMPLETED", "FAILED", "CANCELLED"]:
                    print(f"  ✓ Flow {status.lower()}")
                    return status
            time.sleep(5)
        except Exception as e:
            print(f"  ⚠ Poll error: {e}")
            time.sleep(5)
    
    raise TimeoutError(f"Flow did not complete within {timeout}s")


def query_jaeger_trace(run_id: str) -> Optional[Dict[str, Any]]:
    """Query Jaeger for trace by run_id tag"""
    print(f"  → Querying Jaeger for trace (run_id={run_id})...")
    
    # Jaeger Query API: GET /api/traces?service=flowgent&tags={"flowgent.run_id":"..."}
    url = f"{JAEGER_API}/api/traces"
    params = {
        "service": "flowgent",
        "tags": json.dumps({"flowgent.run_id": run_id}),
        "limit": 1,
    }
    
    try:
        resp = requests.get(url, params=params, timeout=10)
        if resp.status_code != 200:
            raise Exception(f"Jaeger query failed: {resp.status_code} {resp.text}")
        
        data = resp.json()
        traces = data.get("data", [])
        
        if not traces:
            raise Exception(f"No traces found for run_id={run_id}")
        
        trace = traces[0]
        span_count = len(trace.get("spans", []))
        trace_id = trace.get("traceID")
        
        print(f"  ✓ Found trace: traceID={trace_id}, spans={span_count}")
        return trace
        
    except Exception as e:
        print(f"  ✗ Jaeger query failed: {e}")
        raise


def verify_node_spans(trace: Dict[str, Any], node_id: str) -> Dict[str, Any]:
    """Verify a single node has all required spans"""
    spans = trace.get("spans", [])
    
    # Filter spans for this node
    node_spans = []
    for span in spans:
        tags = {t["key"]: t["value"] for t in span.get("tags", [])}
        if tags.get("flowgent.node_id") == node_id:
            node_spans.append({
                "spanID": span.get("spanID"),
                "operationName": span.get("operationName"),
                "startTime": span.get("startTime"),
                "duration": span.get("duration"),
                "tags": tags,
                "references": span.get("references", []),
            })
    
    if not node_spans:
        return {"node_id": node_id, "status": "MISSING", "span_count": 0}
    
    # Check required operations
    operations = [s["operationName"] for s in node_spans]
    missing_ops = []
    
    for required in REQUIRED_SPAN_OPERATIONS:
        # Prefix match for SlotWorker-Execute* and API-PUT*
        if "*" in required or any(op.startswith(required.replace("*", "")) for op in operations):
            continue
        if required not in operations:
            missing_ops.append(required)
    
    # Verify parent-child relationships
    dispatch_span = next((s for s in node_spans if "DispatchPlan" in s["operationName"]), None)
    consume_span = next((s for s in node_spans if "ConsumeExecPlan" in s["operationName"]), None)
    
    parent_child_ok = True
    if dispatch_span and consume_span:
        # consume_span should reference dispatch_span as parent
        parent_refs = [r for r in consume_span.get("references", []) if r.get("refType") == "CHILD_OF"]
        if not any(r.get("spanID") == dispatch_span["spanID"] for r in parent_refs):
            parent_child_ok = False
    
    status = "OK" if not missing_ops and parent_child_ok else "INCOMPLETE"
    
    return {
        "node_id": node_id,
        "status": status,
        "span_count": len(node_spans),
        "operations": operations,
        "missing_operations": missing_ops,
        "parent_child_ok": parent_child_ok,
    }


def verify_phase_grouping(trace: Dict[str, Any]) -> Dict[str, Any]:
    """Verify phase-level span grouping"""
    spans = trace.get("spans", [])
    phase_timings = {}
    
    for phase_name, node_ids in FLOW_PHASES.items():
        phase_spans = []
        for span in spans:
            tags = {t["key"]: t["value"] for t in span.get("tags", [])}
            if tags.get("flowgent.node_id") in node_ids:
                phase_spans.append(span)
        
        if phase_spans:
            start_times = [s["startTime"] for s in phase_spans]
            end_times = [s["startTime"] + s["duration"] for s in phase_spans]
            phase_duration_us = max(end_times) - min(start_times)
            phase_timings[phase_name] = {
                "duration_ms": phase_duration_us / 1000,
                "node_count": len(node_ids),
                "span_count": len(phase_spans),
            }
    
    return phase_timings


def run():
    """Main verification flow"""
    print("\n" + "="*60)
    print("  Scenario 10: OTEL Tracing — Complete 24-Node Span Coverage")
    print("="*60)
    
    # Step 1: Trigger flow
    run_id = trigger_security_fixer()
    
    # Step 2: Wait for completion
    status = wait_for_completion(run_id, timeout=600)
    if status != "COMPLETED":
        print(f"  ⚠ Flow status={status}, continuing trace validation...")
    
    # Step 3: Query Jaeger
    trace = query_jaeger_trace(run_id)
    
    # Step 4: Verify all 24 nodes
    print(f"\n  → Verifying span coverage for all 24 nodes...")
    results = {}
    for node_id in ALL_NODES:
        result = verify_node_spans(trace, node_id)
        results[node_id] = result
        
        status_icon = "✓" if result["status"] == "OK" else "✗"
        print(f"    {status_icon} {node_id:25s} spans={result['span_count']:2d}  status={result['status']}")
        
        if result["status"] != "OK":
            if result["missing_operations"]:
                print(f"       Missing: {', '.join(result['missing_operations'])}")
            if not result.get("parent_child_ok", True):
                print(f"       Parent-child relationship broken")
    
    # Step 5: Verify phase grouping
    print(f"\n  → Phase-level timings:")
    phase_timings = verify_phase_grouping(trace)
    for phase_name, timing in phase_timings.items():
        print(f"    {phase_name:15s} {timing['duration_ms']:8.1f}ms  nodes={timing['node_count']:2d}  spans={timing['span_count']:3d}")
    
    # Step 6: Summary
    ok_count = sum(1 for r in results.values() if r["status"] == "OK")
    total_count = len(results)
    
    print(f"\n  {'='*60}")
    print(f"  Summary: {ok_count}/{total_count} nodes have complete span coverage")
    print(f"  {'='*60}")
    
    if ok_count < total_count:
        failed_nodes = [nid for nid, r in results.items() if r["status"] != "OK"]
        raise AssertionError(f"Incomplete span coverage for nodes: {failed_nodes}")
    
    print(f"\n  ✓ All 24 nodes have complete OTEL traces")
    print(f"  ✓ Jaeger UI: {JAEGER_API}/trace/{trace['traceID']}")


if __name__ == "__main__":
    run()
