"""Scenario 25 — real A2A 0.3 JSON-RPC execution verifier.

This is a strict verifier: the A2A service must be reachable, authenticated,
standards-compatible, backed by the shared task store, and able to drive a real
Flowgent run through caller-scoped APIServer RBAC. It never accepts SKIP as a
passing result.
"""

import json
import time
import uuid
from typing import Any

from common import api as common_api
from common import config


A2A = config.K8S_A2A_URL.rstrip("/")
NAMESPACE = config.NAMESPACE_ID
FLOW_ID = "a2a-e2e-" + uuid.uuid4().hex[:8]
SESSION = common_api.flowgent_session()


def _result_payload(task: dict) -> Any:
    for artifact in task.get("artifacts") or []:
        for part in artifact.get("parts") or []:
            text = part.get("text")
            if isinstance(text, str):
                try:
                    return json.loads(text)
                except json.JSONDecodeError:
                    continue
    raise AssertionError(f"A2A task {task.get('id', '<unknown>')} has no JSON result artifact")


def _rpc(method: str, params: dict) -> dict:
    response = SESSION.post(
        A2A,
        json={"jsonrpc": "2.0", "id": uuid.uuid4().hex, "method": method, "params": params},
        timeout=30,
    )
    assert response.status_code == 200, (
        f"A2A {method} returned HTTP {response.status_code}: {response.text[:300]}"
    )
    envelope = response.json()
    assert not envelope.get("error"), f"A2A {method} error: {envelope['error']}"
    return envelope.get("result") or {}


def _action(action: str, **values) -> tuple[dict, Any]:
    data = {"action": action, "namespace": NAMESPACE, **values}
    task = _rpc(
        "message/send",
        {
            "message": {
                "kind": "message",
                "messageId": uuid.uuid4().hex,
                "role": "user",
                "parts": [{"kind": "data", "data": data}],
            },
            "configuration": {"blocking": True},
        },
    )
    state = (task.get("status") or {}).get("state")
    assert state == "completed", (
        f"A2A action {action} task={task.get('id', '<unknown>')} state={state}"
    )
    task_id = task.get("id")
    assert task_id, f"A2A action {action} did not return a protocol task id"

    persisted = _rpc("tasks/get", {"id": task_id, "historyLength": 5})
    assert persisted.get("id") == task_id, "A2A task was not readable from the shared store"
    assert (persisted.get("status") or {}).get("state") == "completed"
    return task, _result_payload(task)


def _wait_run(run_id: str):
    deadline = time.time() + 5 * 60
    last = ""
    while time.time() < deadline:
        _, run = _action("get_run", run_id=run_id)
        last = run.get("status", "")
        if last == "COMPLETED":
            return
        if last in ("FAILED", "CANCELLED"):
            raise AssertionError(f"A2A-triggered run ended in {last}: {run}")
        time.sleep(2)
    raise AssertionError(f"A2A-triggered run did not complete; last status={last}")


def run():
    print("  Scenario 25: standard A2A JSON-RPC + real FlowRun")

    health = SESSION.get(f"{A2A}/_/healthz", timeout=5)
    assert health.status_code == 200, f"A2A health failed: {health.status_code}"
    card_response = SESSION.get(f"{A2A}/.well-known/agent.json", timeout=5)
    assert card_response.status_code == 200, f"agent card failed: {card_response.status_code}"
    card = card_response.json()
    assert card.get("protocolVersion") == "0.3.0", card
    assert card.get("preferredTransport") == "JSONRPC", card
    assert card.get("securitySchemes", {}).get("bearerAuth", {}).get("scheme") == "Bearer", card
    assert len(card.get("skills") or []) >= 9, card
    print(f"  agent card OK: protocol={card['protocolVersion']} skills={len(card['skills'])}")

    _, flows = _action("list_flows")
    assert isinstance(flows, list), f"A2A list_flows returned {type(flows).__name__}, want list"
    flow_spec = {
        "id": FLOW_ID,
        "kind": "flow",
        "description": "Real A2A protocol execution verifier",
        "runtime_mode": "session",
        "nodes": [
            {
                "id": "a2a-proof",
                "kind": "sandbox",
                "runtime": "bash",
                "timeout": "30s",
                "script": "#!/bin/bash\nset -euo pipefail\nprintf '{\"a2a\":true}\\n'\n",
            }
        ],
        "edges": [],
    }
    try:
        _, created = _action("create_flow", spec=flow_spec)
        assert created.get("agentflow_id") == FLOW_ID, created

        _, triggered = _action(
            "start_run",
            agentflow_id=FLOW_ID,
            vars={"verification": "security-autonomy-fixer/a2a"},
        )
        run_id = triggered.get("run_id")
        assert run_id, f"A2A start_run response omitted run_id: {triggered}"
        _wait_run(run_id)
        print(f"  real A2A FlowRun completed: run_id={run_id}")

        _, runs = _action("list_runs", agentflow_id=FLOW_ID)
        items = runs.get("items") if isinstance(runs, dict) else runs
        assert isinstance(items, list), f"A2A list_runs returned {type(runs).__name__}, want list"
        assert any(item.get("id") == run_id for item in (items or [])), runs
    finally:
        try:
            _action("delete_flow", agentflow_id=FLOW_ID)
        except Exception as exc:
            print(f"  WARN: A2A verifier cleanup failed: {exc}")

    print("  PASS: authenticated standard A2A, shared tasks, and real execution verified")
