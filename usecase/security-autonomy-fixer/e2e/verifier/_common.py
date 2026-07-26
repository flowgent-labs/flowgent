"""Shared utilities for E2E security fixer sub-verifier scenarios."""

import os
import yaml

from common import config
from common import unwrap_k8s, resolve_env_vars, get_or_post, tasks_by_node
from common import parse_output, node_task, pg_connect
from common.api import get_tasks as _get_tasks_raw
from common.api import try_approve_pending_human as _try_approve_raw

API = config.K8S_APISERVER_URL
NAMESPACE = config.K8S_NAMESPACE
FLOW_ID = "security-autonomy-fixer"
FLOW_TIMEOUT_S = config.FLOW_TIMEOUT_S
POLL_INTERVAL_S = config.POLL_INTERVAL_S

_CONFIG_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "config")
_FLOW_YAML_PATH = os.path.join(_CONFIG_ROOT, "flows", "security-autonomy-fixer.yaml")
_AGENTS_DIR = os.path.join(_CONFIG_ROOT, "agents")

_USE_REAL_MCP = os.getenv("FLOWGENT_E2E_USE_REAL_MCP", "true").lower() == "true"
_MOCK_MCP_COMMAND = ["/app/mcp-server.sh"]

AGENT_NAMES = ["supervisor", "issue-detector", "fixer-agent",
               "security-reviewer", "quality-reviewer", "arch-reviewer", "git-agent"]
MCP_NAMES = ["github", "sonarqube"]

PHASE_NODES = {
    "DISCOVERY": ["get-commit", "scan-sonarqube"],
    "ANALYZE": ["aggregate-issues"],
    "FIX": ["generate-fixes"],
    "REVIEW": ["review-security", "review-quality", "review-arch"],
    "VOTE": ["committee"],
    "SUPERVISOR": ["supervisor-check"],
    "CONDITION": ["is-approved"],
    "HUMAN": ["human-approval"],
    "COMMIT_PR": ["create-branch", "commit-fixes", "create-pr"],
    "RESCAN": ["trigger-rescan", "wait-rescan", "check-resolved", "compare-results", "fix-complete"],
    "REPORT": ["summary-report", "notify-pr", "notify-email", "notify-teams", "end"],
}


def get_tasks(s, run_id):
    return _get_tasks_raw(s, API, NAMESPACE, run_id)


def try_approve_pending_human(s, run_id, conn=None):
    return _try_approve_raw(s, API, run_id, conn)


def seed_agents_and_mcps(s):
    print("  -> Seeding agent definitions...")
    agent_count = 0
    for fname in sorted(os.listdir(_AGENTS_DIR)):
        if not fname.endswith(".yaml"):
            continue
        with open(os.path.join(_AGENTS_DIR, fname)) as f:
            agent_def = unwrap_k8s(yaml.safe_load(f))
        name = agent_def.get("name")
        if not name:
            continue
        if get_or_post(s, API, f"/api/v1/{NAMESPACE}/agents/{name}",
                       f"/api/v1/{NAMESPACE}/agents", agent_def, "agent"):
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
                mcp_def = unwrap_k8s(yaml.safe_load(f))
            mcp_def = resolve_env_vars(mcp_def)
        else:
            mcp_def = {"name": mode, "enabled": True, "type": "stdio",
                       "command": _MOCK_MCP_COMMAND, "args": [mode], "env": {}}
        if get_or_post(s, API, f"/api/v1/{NAMESPACE}/mcp/{mode}",
                       f"/api/v1/{NAMESPACE}/mcp", mcp_def, "mcp"):
            mcp_count += 1
    print(f"  OK {mcp_count} MCP server(s) registered "
          f"({'real config/mcps/*.yaml' if _USE_REAL_MCP else 'mock /app/mcp-server.sh'})")


def load_flow_from_yaml():
    with open(_FLOW_YAML_PATH) as f:
        data = unwrap_k8s(yaml.safe_load(f))
    for key in ("triggers",):
        data.pop(key, None)
    return data
