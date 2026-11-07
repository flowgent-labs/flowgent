"""
Flowgent E2E — common utilities package.

Re-exports all shared symbols so callers can import from 'common' directly.
"""

from .paths import (
    E2E_DIR, USE_CASE_DIR, PROJECT_ROOT,
    HELM_CHART, SONAR_COMPOSE, CONFIG_DIR, CONSOLE_BIN, CONSOLE_CFG,
)
from .shell import run_cmd
from .api import (
    unwrap_k8s, resolve_env_vars, get_or_post,
    get_tasks, tasks_by_node, parse_output, node_task,
    try_approve_pending_human,
)
from .db import pg_connect

# config is a sub-module — import it so `common.config` works
from . import config
