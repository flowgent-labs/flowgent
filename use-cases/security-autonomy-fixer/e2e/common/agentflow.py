"""Shared utilities for E2E security fixer sub-verifier scenarios."""
from __future__ import annotations

import base64
import atexit
import hashlib
import json
import os
import re
import subprocess
import time
import yaml
import requests

from common import config
from common.config import CONFIG_DIR
from common.project import (
    FlowgentE2EProject,
    MQTT_AUDIT_PATH,
    PR_BASELINE_PATH,
)

API = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID
SYSTEM_NAMESPACE = config.SYSTEM_NAMESPACE
WORKLOAD_NAMESPACE = config.K8S_WORKLOAD_NAMESPACE
FLOW_ID = "security-autonomy-fixer"
FLOW_TIMEOUT_S = config.FLOW_TIMEOUT_S
POLL_INTERVAL_S = config.POLL_INTERVAL_S

_CONFIG_ROOT = CONFIG_DIR
_FLOW_YAML_PATH = os.path.join(_CONFIG_ROOT, "flows", "security-autonomy-fixer.yaml")
_AGENTS_DIR = os.path.join(_CONFIG_ROOT, "agents")
GITHUB_API = "https://api.github.com"
PR_REPO = "wl4g/rengine"
PR_NUMBER = 4

_USE_REAL_MCP = os.getenv("FLOWGENT_E2E_USE_REAL_MCP", "true").lower() == "true"
_MOCK_MCP_COMMAND = ["/app/mcp-server.sh"]

AGENT_NAMES = ["supervisor", "issue-detector", "fixer-agent",
               "security-reviewer", "quality-reviewer", "arch-reviewer", "git-agent"]
MCP_NAMES = ["github", "sonarqube"]

PHASE_NODES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues", "git-clone", "read-source-files"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["committee"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["check-existing-pr", "pr-exists", "create-branch", "commit-fixes", "create-pr", "commit-to-existing"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "end"],
}

COMPLETED_STATUSES = {"COMPLETED", "SUCCESS"}

class SecurityAutonomyFixture:
    """Shared Flowgent security-autonomy-fixer fixture and scenario operations."""

    API = API
    NAMESPACE = NAMESPACE
    SYSTEM_NAMESPACE = SYSTEM_NAMESPACE
    WORKLOAD_NAMESPACE = WORKLOAD_NAMESPACE
    FLOW_ID = FLOW_ID
    FLOW_TIMEOUT_S = FLOW_TIMEOUT_S
    POLL_INTERVAL_S = POLL_INTERVAL_S
    GITHUB_API = GITHUB_API
    PR_REPO = PR_REPO
    PR_NUMBER = PR_NUMBER
    AGENT_NAMES = AGENT_NAMES
    MCP_NAMES = MCP_NAMES
    PHASE_NODES = PHASE_NODES
    COMPLETED_STATUSES = COMPLETED_STATUSES
    MQTT_AUDIT_PATH = MQTT_AUDIT_PATH
    node_task = staticmethod(FlowgentE2EProject.node_task)
    parse_output = staticmethod(FlowgentE2EProject.parse_output)
    pg_connect = staticmethod(FlowgentE2EProject.pg_connect)
    tasks_by_node = staticmethod(FlowgentE2EProject.tasks_by_node)

    @staticmethod
    def github_headers():
        headers = {
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
        }
        token = os.getenv("GITHUB_TOKEN") or os.getenv("GH_TOKEN") or ""
        if token:
            headers["Authorization"] = f"Bearer {token}"
        return headers

    @staticmethod
    def gh_get_json(path, timeout=15):
        resp = requests.get(f"{GITHUB_API}{path}", headers=SecurityAutonomyFixture.github_headers(), timeout=timeout)
        if resp.status_code != 200:
            raise AssertionError(f"GitHub API returned {resp.status_code} for {path}: {resp.text[:200]}")
        return resp.json()

    @staticmethod
    def fetch_pr_commits(repo=PR_REPO, pr_number=PR_NUMBER):
        commits = []
        page = 1
        while page <= 10:
            data = SecurityAutonomyFixture.gh_get_json(f"/repos/{repo}/pulls/{pr_number}/commits?per_page=100&page={page}")
            if not isinstance(data, list):
                raise AssertionError(f"Unexpected commits response type: {type(data).__name__}")
            commits.extend(data)
            if len(data) < 100:
                break
            page += 1
        return commits

    @staticmethod
    def capture_pr_baseline():
        commits = SecurityAutonomyFixture.fetch_pr_commits()
        baseline = {
            "repo": PR_REPO,
            "pr_number": PR_NUMBER,
            "commit_count": len(commits),
            "head_sha": commits[-1].get("sha", "") if commits else "",
            "shas": [commit.get("sha", "") for commit in commits if commit.get("sha")],
            "captured_at": time.time(),
        }
        with open(PR_BASELINE_PATH, "w") as f:
            json.dump(baseline, f, indent=2, sort_keys=True)
        return baseline

    @staticmethod
    def load_pr_baseline():
        if not os.path.isfile(PR_BASELINE_PATH):
            raise AssertionError("No .last_pr_baseline.json found — run Scenario 31 before Scenario 35")
        with open(PR_BASELINE_PATH) as f:
            return json.load(f)

    @staticmethod
    def get_tasks(s, run_id):
        return FlowgentE2EProject.get_tasks(s, API, NAMESPACE, run_id)

    @staticmethod
    def try_approve_pending_human(s, run_id, conn=None):
        return FlowgentE2EProject.try_approve_pending_human(s, API, NAMESPACE, run_id, conn)

    @staticmethod
    def seed_agents_and_mcps(s):
        print("  -> Seeding agent definitions...")
        agent_count = 0
        for fname in sorted(os.listdir(_AGENTS_DIR)):
            if not fname.endswith(".yaml"):
                continue
            with open(os.path.join(_AGENTS_DIR, fname)) as f:
                agent_def = FlowgentE2EProject.unwrap_k8s(yaml.safe_load(f))
            name = agent_def.get("name")
            if not name:
                continue
            existing = s.get(f"{API}/api/v1/{NAMESPACE}/agents/{name}")
            if existing.status_code == 200:
                updated = s.put(f"{API}/api/v1/{NAMESPACE}/agents/{name}", json=agent_def)
                ok = updated.status_code in (200, 204)
                if not ok:
                    print(f"  WARN: failed to update agent {name}: {updated.status_code} {updated.text[:160]}")
            else:
                ok = FlowgentE2EProject.get_or_post(
                    s,
                    API,
                    f"/api/v1/{NAMESPACE}/agents/{name}",
                    f"/api/v1/{NAMESPACE}/agents",
                    agent_def,
                    "agent",
                )
            if ok:
                agent_count += 1
        print(f"  OK {agent_count} agent definition(s) registered (from {_AGENTS_DIR})")

        if not os.environ.get("GITHUB_TOKEN") and os.environ.get("GH_TOKEN"):
            os.environ["GITHUB_TOKEN"] = os.environ["GH_TOKEN"]

        print("  -> Seeding MCP definitions...")
        mcp_count = 0
        for mode in ("github", "sonarqube"):
            if _USE_REAL_MCP:
                mcp_path = os.path.join(_CONFIG_ROOT, "mcps", f"{mode}.yaml")
                with open(mcp_path) as f:
                    mcp_def = FlowgentE2EProject.unwrap_k8s(yaml.safe_load(f))
            else:
                mcp_def = {"name": mode, "enabled": True, "type": "stdio",
                           "command": _MOCK_MCP_COMMAND, "args": [mode], "env": {}}
            mcp_def["enabled"] = True
            existing = s.get(f"{API}/api/v1/{NAMESPACE}/mcp/{mode}")
            if existing.status_code == 200:
                updated = s.put(f"{API}/api/v1/{NAMESPACE}/mcp/{mode}", json=mcp_def)
                ok = updated.status_code in (200, 204)
                if not ok:
                    print(f"  WARN: failed to enable mcp {mode}: {updated.status_code} {updated.text[:160]}")
            else:
                ok = FlowgentE2EProject.get_or_post(s, API, f"/api/v1/{NAMESPACE}/mcp/{mode}",
                                 f"/api/v1/{NAMESPACE}/mcp", mcp_def, "mcp")
            if ok:
                mcp_count += 1
        print(f"  OK {mcp_count} MCP server(s) registered "
              f"({'real config/mcps/*.yaml' if _USE_REAL_MCP else 'mock /app/mcp-server.sh'})")

    @staticmethod
    def load_flow_from_yaml():
        with open(_FLOW_YAML_PATH) as f:
            data = FlowgentE2EProject.resolve_env_vars(FlowgentE2EProject.unwrap_k8s(yaml.safe_load(f)))
        for key in ("name", "triggers"):
            data.pop(key, None)
        data["kind"] = "flow"
        return data

    @staticmethod
    def upsert_flow(session, flow_def):
        """Update an imported flow in place, or create it when genuinely absent."""
        path = f"{API}/api/v1/{NAMESPACE}/flows/{FLOW_ID}"
        existing = session.get(path)
        if existing.status_code == 200:
            return session.put(path, json=flow_def)
        return session.post(f"{API}/api/v1/{NAMESPACE}/flows", json=flow_def)

    @staticmethod
    def workload_namespace(namespace_id=NAMESPACE):
        return f"{config.K8S_WORKLOAD_NAMESPACE_PREFIX}{namespace_id}"

    @staticmethod
    def _kubectl_json(args):
        result = subprocess.run(["kubectl", *args, "-o", "json"], capture_output=True, text=True, timeout=20)
        if result.returncode != 0:
            return None
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return None

    @staticmethod
    def _pod_ready(pod):
        if pod.get("status", {}).get("phase") != "Running":
            return False
        statuses = pod.get("status", {}).get("containerStatuses", [])
        return bool(statuses) and all(c.get("ready") for c in statuses)

    @staticmethod
    def wait_for_pods(namespace, selector, label, min_count=1, timeout=180):
        print(f"  -> Waiting for {label}: namespace={namespace}, selector={selector}, min={min_count}")
        deadline = time.time() + timeout
        last = []
        while time.time() < deadline:
            pods = SecurityAutonomyFixture._kubectl_json(["get", "pods", "-n", namespace, "-l", selector])
            items = pods.get("items", []) if pods else []
            ready = [p for p in items if SecurityAutonomyFixture._pod_ready(p)]
            last = [
                f"{p['metadata']['name']}:{p.get('status', {}).get('phase', '?')}"
                for p in items
            ]
            if len(ready) >= min_count:
                for p in ready:
                    print(f"     OK {p['metadata']['name']} ready")
                return ready
            time.sleep(3)
        raise AssertionError(f"{label} pods not ready within {timeout}s; last={last}")

    @staticmethod
    def runtime_mode_for_flow(flow_id=FLOW_ID):
        response = FlowgentE2EProject.session().get(f"{API}/api/v1/{NAMESPACE}/flows/{flow_id}", timeout=10)
        if response.status_code != 200:
            raise AssertionError(f"cannot resolve runtime_mode for {flow_id}: HTTP {response.status_code}")
        mode = response.json().get("runtime_mode")
        if mode not in ("application", "session"):
            raise AssertionError(f"Flow {flow_id} has invalid runtime_mode={mode!r}")
        return mode

    @staticmethod
    def _kubernetes_name(*parts):
        raw = "-".join(parts)
        lower = raw.lower()
        normalized = []
        previous_hyphen = False
        needs_hash = False
        for char in lower:
            if "a" <= char <= "z" or "0" <= char <= "9":
                normalized.append(char)
                previous_hyphen = False
                continue
            if char != "-":
                needs_hash = True
            if not previous_hyphen:
                normalized.append("-")
                previous_hyphen = True
        base = re.sub(r"^-+|-+$", "", "".join(normalized))
        if not base:
            base = "flowgent"
            needs_hash = True
        if len(base) > 63:
            needs_hash = True
        if not needs_hash:
            return base
        digest = hashlib.sha256(raw.lower().encode()).hexdigest()[:10]
        max_base = 63 - 1 - len(digest)
        return base[:max_base].rstrip("-") + "-" + digest

    @staticmethod
    def runtime_cluster_for_run(run_id):
        if not run_id:
            raise AssertionError("run_id is required to resolve application runtime cluster")
        return SecurityAutonomyFixture._kubernetes_name("app", run_id)

    @staticmethod
    def wait_for_workload_components(flow_id=FLOW_ID, run_id=None, timeout=240):
        if os.getenv("FLOWGENT_E2E_DEPLOYER") == "docker":
            print(f"  -> Verifying inline Docker runtime for run={run_id}")
            deadline = time.time() + min(timeout, 60)
            last_status = ""
            session = FlowgentE2EProject.session()
            while time.time() < deadline:
                response = session.get(
                    f"{API}/api/v1/{NAMESPACE}/runs/{run_id}", timeout=10
                )
                if response.status_code == 200:
                    last_status = response.json().get("status", "")
                    if last_status and last_status != "PENDING":
                        print(f"     OK inline runtime accepted run: status={last_status}")
                        return
                time.sleep(1)
            raise AssertionError(
                f"Docker inline runtime did not accept run within 60s; status={last_status}"
            )
        mode = SecurityAutonomyFixture.runtime_mode_for_flow(flow_id)
        if mode == "session":
            cluster_id = os.getenv("FLOWGENT_SESSION_CLUSTER_ID", "session")
            ns = SYSTEM_NAMESPACE
            SecurityAutonomyFixture.wait_for_pods(ns, f"app.kubernetes.io/component=session-jobmanager,flowgent.io/runtime-cluster={cluster_id}", "Session JobManager", 1, timeout)
            SecurityAutonomyFixture.wait_for_pods(ns, f"flowgent/role=worker,flowgent.io/runtime-cluster={cluster_id}", "Session TaskManager", 1, timeout)
            SecurityAutonomyFixture.wait_for_pods(ns, f"flowgent/role=sandbox-worker,flowgent.io/runtime-cluster={cluster_id}", "Session Sandbox", 1, timeout)
            return
        cluster_id = SecurityAutonomyFixture.runtime_cluster_for_run(run_id)
        ns = SecurityAutonomyFixture.workload_namespace()
        SecurityAutonomyFixture.wait_for_pods(ns, f"flowgent.io/runtime-boundary=flow-jobmanager,flowgent.io/flow={flow_id},flowgent.io/run={run_id},flowgent.io/runtime-cluster={cluster_id}", "Application JobManager", 1, timeout)
        SecurityAutonomyFixture.wait_for_pods(ns, f"flowgent/role=worker,flowgent.io/runtime-cluster={cluster_id}", "Application TaskManager", 1, timeout)
        SecurityAutonomyFixture.wait_for_pods(ns, f"flowgent/role=sandbox-worker,flowgent.io/runtime-cluster={cluster_id}", "Application Sandbox", 1, timeout)

    @staticmethod
    def decode_mqtt_payload(raw_payload):
        try:
            envelope = json.loads(raw_payload.decode() if isinstance(raw_payload, (bytes, bytearray)) else raw_payload)
        except Exception:
            return {"_raw": raw_payload.decode(errors="replace") if isinstance(raw_payload, (bytes, bytearray)) else str(raw_payload)}
        if isinstance(envelope, dict) and "payload" in envelope:
            payload = envelope.get("payload")
            if isinstance(payload, str):
                try:
                    decoded = base64.b64decode(payload).decode()
                    return json.loads(decoded)
                except Exception:
                    try:
                        return json.loads(payload)
                    except Exception:
                        return {"payload": payload}
            if isinstance(payload, dict):
                return payload
        return envelope

    @staticmethod
    def start_global_mqtt_audit(run_id=None):
        global _GLOBAL_MQTT_AUDIT
        SecurityAutonomyFixture.stop_global_mqtt_audit()
        _GLOBAL_MQTT_AUDIT = MQTTAudit(run_id).start()
        return _GLOBAL_MQTT_AUDIT

    @staticmethod
    def ensure_global_mqtt_audit(run_id=None):
        global _GLOBAL_MQTT_AUDIT
        if _GLOBAL_MQTT_AUDIT is None:
            _GLOBAL_MQTT_AUDIT = MQTTAudit(run_id).start()
        elif run_id:
            _GLOBAL_MQTT_AUDIT.set_run_id(run_id)
        return _GLOBAL_MQTT_AUDIT

    @staticmethod
    def snapshot_global_mqtt_audit(run_id=None):
        if _GLOBAL_MQTT_AUDIT is None:
            return []
        if run_id:
            _GLOBAL_MQTT_AUDIT.set_run_id(run_id)
        return _GLOBAL_MQTT_AUDIT.snapshot()

    @staticmethod
    def stop_global_mqtt_audit(run_id=None):
        global _GLOBAL_MQTT_AUDIT
        if _GLOBAL_MQTT_AUDIT is None:
            return []
        if run_id:
            _GLOBAL_MQTT_AUDIT.set_run_id(run_id)
        messages = _GLOBAL_MQTT_AUDIT.stop()
        _GLOBAL_MQTT_AUDIT = None
        return messages

    @staticmethod
    def _mqtt_message_key(message):
        payload = message.get("payload")
        try:
            payload_key = json.dumps(payload, sort_keys=True, separators=(",", ":"))
        except TypeError:
            payload_key = str(payload)
        return (message.get("run_id"), message.get("topic"), payload_key)

    @staticmethod
    def redact_mqtt_evidence(value, parent_key=""):
        """Preserve MQTT structure for assertions without persisting credentials."""
        normalized_parent = parent_key.lower().replace("-", "_")
        if isinstance(value, dict):
            if normalized_parent in ("env", "environment"):
                return {str(key): "<redacted>" for key in value}
            redacted = {}
            for key, item in value.items():
                normalized_key = str(key).lower().replace("-", "_")
                if any(part in normalized_key for part in _SENSITIVE_EVIDENCE_KEY_PARTS):
                    redacted[key] = "<redacted>"
                else:
                    redacted[key] = SecurityAutonomyFixture.redact_mqtt_evidence(item, normalized_key)
            return redacted
        if isinstance(value, list):
            return [SecurityAutonomyFixture.redact_mqtt_evidence(item, normalized_parent) for item in value]
        return value

    @staticmethod
    def save_mqtt_audit(run_id, messages):
        existing = SecurityAutonomyFixture.load_mqtt_audit(run_id, strict=False)
        merged = []
        seen = set()
        for message in existing + [m for m in messages if not run_id or m.get("run_id") in (None, run_id)]:
            message = SecurityAutonomyFixture.redact_mqtt_evidence(message)
            key = SecurityAutonomyFixture._mqtt_message_key(message)
            if key in seen:
                continue
            seen.add(key)
            merged.append(message)
        with open(MQTT_AUDIT_PATH, "w") as f:
            json.dump({"run_id": run_id, "messages": merged}, f, indent=2, sort_keys=True)
        print(f"  OK MQTT audit saved: {len(merged)} message(s) -> .last_mqtt_audit.json")

    @staticmethod
    def load_mqtt_audit(run_id, strict=True):
        if not os.path.isfile(MQTT_AUDIT_PATH):
            if strict:
                raise AssertionError("No .last_mqtt_audit.json found")
            return []
        with open(MQTT_AUDIT_PATH) as f:
            data = json.load(f)
        if strict and data.get("run_id") != run_id:
            raise AssertionError(f"MQTT audit run_id mismatch: {data.get('run_id')} != {run_id}")
        return data.get("messages", [])

    @staticmethod
    def assert_mqtt_suffixes(run_id, required_suffixes):
        messages = SecurityAutonomyFixture.load_mqtt_audit(run_id)
        topics = [m.get("topic", "") for m in messages]
        missing = []
        for suffix in required_suffixes:
            count = sum(1 for t in topics if t.endswith(suffix))
            print(f"  MQTT audit {suffix}: {count}")
            if count == 0:
                missing.append(suffix)
        if missing:
            raise AssertionError(f"Missing non-$share MQTT observations for run {run_id}: {missing}")
        return messages

    @staticmethod
    def is_inline_runtime():
        return os.getenv("FLOWGENT_E2E_DEPLOYER", "k8s") == "docker"

    @staticmethod
    def execution_audit_wakeup_suffix():
        """First backend-specific message proving execution left the API layer."""
        return "sandbox/trigger" if SecurityAutonomyFixture.is_inline_runtime() else "exec/plans"

    @staticmethod
    def assert_runtime_execution_evidence(
        run_id, tasks, *, required_nodes=None, require_sandbox=False
    ):
        """Verify equivalent execution evidence for inline and distributed modes.

        Kubernetes dispatches every task over exec/plans and exec/results. Docker
        intentionally executes RM/TM in-process, so persisted TaskRun state is the
        authoritative equivalent while sandbox work still crosses MQTT.
        """
        required_nodes = set(required_nodes or [])
        completed_nodes = {
            SecurityAutonomyFixture.task_node_key(task)
            for task in tasks
            if SecurityAutonomyFixture.task_node_key(task)
            and task.get("status") in COMPLETED_STATUSES
        }
        missing_nodes = sorted(required_nodes - completed_nodes)
        if missing_nodes:
            raise AssertionError(f"Required completed TaskRun evidence missing: {missing_nodes}")

        if SecurityAutonomyFixture.is_inline_runtime():
            if not completed_nodes:
                raise AssertionError("Docker inline runtime persisted no completed TaskRuns")
            suffixes = ["sandbox/trigger", "sandbox/result"] if require_sandbox else []
            messages = SecurityAutonomyFixture.assert_mqtt_suffixes(run_id, suffixes) if suffixes else SecurityAutonomyFixture.load_mqtt_audit(run_id)
            print(f"  Inline RM/TM completed TaskRun evidence: {len(completed_nodes)} node(s)")
            return messages

        suffixes = ["exec/plans", "exec/results"]
        if require_sandbox:
            suffixes.extend(["sandbox/trigger", "sandbox/result"])
        messages = SecurityAutonomyFixture.assert_mqtt_suffixes(run_id, suffixes)
        SecurityAutonomyFixture.assert_completed_task_mqtt_coverage(run_id, tasks)
        return messages

    @staticmethod
    def task_completed(task):
        return bool(task) and task.get("status") in COMPLETED_STATUSES

    @staticmethod
    def task_output(task):
        return FlowgentE2EProject.parse_output(task) if task else {}

    @staticmethod
    def message_payload(message):
        payload = message.get("payload", {}) if isinstance(message, dict) else {}
        if isinstance(payload, dict) and isinstance(payload.get("payload"), str):
            try:
                return json.loads(payload["payload"])
            except Exception:
                return payload
        return payload if isinstance(payload, dict) else {}

    @staticmethod
    def message_node_id(message):
        payload = SecurityAutonomyFixture.message_payload(message)
        return payload.get("node_key") or payload.get("node_id") or payload.get("nodeId")

    @staticmethod
    def task_node_key(task):
        """Return the canonical NodeRun key, accepting the retired API alias."""
        return task.get("node_key") or task.get("node_id")

    @staticmethod
    def assert_completed_task_mqtt_coverage(run_id, tasks):
        messages = SecurityAutonomyFixture.load_mqtt_audit(run_id)
        plan_nodes = {
            SecurityAutonomyFixture.message_node_id(m)
            for m in messages
            if m.get("topic", "").endswith("exec/plans")
        }
        result_nodes = {
            SecurityAutonomyFixture.message_node_id(m)
            for m in messages
            if m.get("topic", "").endswith("exec/results")
        }
        plan_nodes.discard(None)
        result_nodes.discard(None)
        completed_nodes = {
            SecurityAutonomyFixture.task_node_key(t)
            for t in tasks
            if SecurityAutonomyFixture.task_node_key(t)
            and t.get("status") in COMPLETED_STATUSES
        }
        missing_plans = sorted(completed_nodes - plan_nodes)
        missing_results = sorted(completed_nodes - result_nodes)
        print(f"  MQTT completed-node plan coverage: {len(completed_nodes) - len(missing_plans)}/{len(completed_nodes)}")
        print(f"  MQTT completed-node result coverage: {len(completed_nodes) - len(missing_results)}/{len(completed_nodes)}")
        if missing_plans or missing_results:
            raise AssertionError(
                "Missing non-$share MQTT observations for completed nodes: "
                f"plans={missing_plans}, results={missing_results}"
            )
        return messages

class MQTTAudit:
    """Non-$share MQTT observer used only for verification."""

    def __init__(self, run_id=None):
        import paho.mqtt.client as mqtt

        self.run_id = run_id
        self.messages = []
        self.client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2)
        self.client.on_message = self._on_message

    def _on_message(self, client, userdata, msg):
        if "$share/" in msg.topic:
            return
        payload = SecurityAutonomyFixture.decode_mqtt_payload(msg.payload)
        run_id = self._run_id_from_topic(msg.topic)
        if self.run_id and run_id and run_id != self.run_id:
            return
        self.messages.append({
            "ts": time.time(),
            "topic": msg.topic,
            "run_id": run_id,
            "payload": payload,
        })

    @staticmethod
    def _run_id_from_topic(topic):
        parts = topic.split("/")
        try:
            idx = parts.index("runs")
            return parts[idx + 1]
        except (ValueError, IndexError):
            return None

    def start(self):
        topics = [
            f"flowgent/v1/{NAMESPACE}/flows/{FLOW_ID}/runs/+/ctrl/run/created",
            f"flowgent/v1/{NAMESPACE}/flows/{FLOW_ID}/runs/+/ctrl/run/status",
            f"flowgent/v1/{NAMESPACE}/clusters/+/flows/{FLOW_ID}/runs/+/exec/plans",
            f"flowgent/v1/{NAMESPACE}/flows/{FLOW_ID}/runs/+/exec/results",
            f"flowgent/v1/{NAMESPACE}/clusters/+/flows/{FLOW_ID}/runs/+/sandbox/trigger",
            f"flowgent/v1/{NAMESPACE}/flows/{FLOW_ID}/runs/+/sandbox/result",
            "flowgent/v1/heartbeat/+",
        ]
        for topic in topics:
            if topic.startswith("$share/"):
                raise AssertionError(f"Verifier MQTT audit must not use shared subscriptions: {topic}")
        self.client.connect(config.EMQX_HOST, config.EMQX_PORT, 20)
        self.client.loop_start()
        for topic in topics:
            self.client.subscribe(topic, qos=1)
            print(f"     audit subscribe: {topic}")
        time.sleep(0.5)
        return self

    def stop(self):
        self.client.loop_stop()
        self.client.disconnect()
        return self.messages

    def snapshot(self):
        return list(self.messages)

    def wait_for(self, suffix, timeout=30):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if any(m["topic"].endswith(suffix) for m in self.messages):
                return True
            time.sleep(0.5)
        return False

    def set_run_id(self, run_id):
        self.run_id = run_id

_GLOBAL_MQTT_AUDIT = None

atexit.register(SecurityAutonomyFixture.stop_global_mqtt_audit)

_SENSITIVE_EVIDENCE_KEY_PARTS = (
    "secret",
    "token",
    "password",
    "passwd",
    "authorization",
    "credential",
    "cookie",
    "api_key",
    "apikey",
    "private_key",
)
