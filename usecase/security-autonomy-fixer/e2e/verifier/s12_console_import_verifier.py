"""
Scenario 12 — Console Import: Binary, Import Command, DB Verification.

Validates the FlowgentConsole CLI management tool's ability to import all
resource types (agents, flows, MCPs, LLM providers, notifiers, skills) from
config/**/*.yaml into the PostgreSQL database.

Steps with Expected I/O:
  L1 — Binary Check
    Step 1.1 Binary exists
      Action:  stat <binary_path>
      Input:   Project built with `make build:core`
      Output:  flowgent-core binary found and executable

    Step 1.2 Binary has console import
      Action:  flowgent-core console import --help
      Input:   Binary with console package linked
      Output:  Help text or known usage; nonzero exit accepted (just need the subcommand)

  L2 — Config Import
    Step 2.1 Import config YAMLs via console
      Action:  flowgent-core --config <cfg> console import <config_dir>
      Input:   PG is reachable, config YAMLs are valid K8s-style resources
      Output:  Per-file "OK" messages for each imported resource type

    Step 2.2 Import summary
      Action:  Count OK lines from console stdout
      Input:   Import completed
      Output:  ≥10 resources imported (7 agents + 1 flow + 1 llm + 2 mcps + 2 notifiers + 2 skills = 15 YAMLs)

  L3 — Database Verification
    Step 3.1 Agents in llm_agent
      Action:  psql -c "SELECT COUNT(*) FROM llm_agent WHERE namespace_id='default'"
      Input:   Import succeeded
      Output:  ≥5 agent records (supervisor, issue-detector, fixer-agent, security-reviewer, quality-reviewer, arch-reviewer, git-agent)

    Step 3.2 Flows in orh_agentflow
      Action:  psql -c "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND kind='flow'"
      Input:   Flow YAMLs imported
      Output:  ≥1 flow record

    Step 3.3 MCPs in llm_mcp
      Action:  psql -c "SELECT COUNT(*) FROM llm_mcp WHERE namespace_id='default'"
      Input:   MCP YAMLs imported
      Output:  ≥2 MCP records

    Step 3.4 LLM Providers in llm_providers
      Action:  psql -c "SELECT COUNT(*) FROM llm_providers WHERE namespace_id='default'"
      Input:   LLM provider YAMLs imported
      Output:  ≥1 LLM provider record

    Step 3.5 Notify Channels in nfy_channel
      Action:  psql -c "SELECT COUNT(*) FROM nfy_channel WHERE namespace_id='default'"
      Input:   Notifier YAMLs imported
      Output:  ≥2 channel records

    Step 3.6 Skills in orh_agentflow
      Action:  psql -c "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND kind='skill'"
      Input:   Skill YAMLs imported
      Output:  ≥2 skill records

    Step 3.8 No idle Flow/Pool runtime
      Action:  kubectl get deployments/pods -A for Flow JM and Resource Pool labels
      Input:   Config import completed, no FlowRun triggered yet
      Output:  No Flow JobManager or Resource Pool worker resources exist
"""

import subprocess
import sys
import os
import json
from common import config

NAMESPACE = config.NAMESPACE_ID

# Paths relative to project root (e2e/verifier -> .. -> project root = ../../../../)
PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))
BINARY_PATHS = [
    os.path.join(PROJECT_ROOT, "bin", "flowgent-core"),
    os.path.join(PROJECT_ROOT, "bin", "flowgent"),
    "flowgent-core",
]
CONFIG_PATHS = [
    os.path.join(PROJECT_ROOT, "etc", "flowgent.yaml"),
    os.path.join(os.path.dirname(__file__), "..", "config", "flowgent.yaml"),
]
CONFIG_DIR = os.path.join(os.path.dirname(__file__), "..", "..", "config")

# Resource counts expected after import
MIN_AGENTS = 5
MIN_FLOWS = 1
MIN_MCPS = 2
MIN_LLM_PROVIDERS = 1
MIN_SKILLS = 2
UI_PROVISION_MODE = os.getenv("FLOWGENT_E2E_PROVISION_MODE", "console").lower() == "ui"
MIN_NOTIFIERS = 1 if UI_PROVISION_MODE else 2
UI_EVIDENCE_PATH = os.path.join(os.path.dirname(__file__), "..", ".last_ui_provision.json")
RUN_ID_PATH = os.path.join(os.path.dirname(__file__), "..", ".last_run_id")
PROXY_ALLOWLIST_ENV = "FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY"
os.environ.setdefault(PROXY_ALLOWLIST_ENV, "github.com")


def _pg_query(query):
    """Run a SQL query via psql in docker PG container and return stdout."""
    env = {**os.environ, "PGPASSWORD": "test"}
    escaped = query.replace("'", "'\"'\"'")
    result = subprocess.run(
        ["sudo", "docker", "exec", "sigbot_e2e_164364_postgres", "sh", "-c",
         f"PGPASSWORD=test psql -U test -d flowgent -XAt -c '{escaped}'"],
        capture_output=True, text=True, timeout=15,
        env=env,
    )
    return result


def _find_binary():
    for bp in BINARY_PATHS:
        if os.path.isfile(bp) and os.access(bp, os.X_OK):
            return bp
    # try which
    result = subprocess.run(["which", "flowgent-core"], capture_output=True, text=True)
    if result.returncode == 0 and result.stdout.strip():
        return result.stdout.strip()
    return None


def _find_config():
    for cp in CONFIG_PATHS:
        if os.path.isfile(cp):
            return cp
    return None


def _kubectl_json(args):
    result = subprocess.run(["kubectl", *args, "-o", "json"], capture_output=True, text=True, timeout=15)
    if result.returncode != 0:
        return None, result.stderr.strip()[:200]
    try:
        return json.loads(result.stdout or "{}"), ""
    except json.JSONDecodeError as exc:
        return None, str(exc)


def _names(items):
    return [
        f"{item.get('metadata', {}).get('namespace', '?')}/{item.get('metadata', {}).get('name', '?')}"
        for item in items
    ]


def _verify_no_idle_workload_runtime(failures):
    checks = [
        (
            "JM deployments",
            ["get", "deployments", "-A", "-l", "app=flowgent-jobmanager"],
        ),
        (
            "JM pods",
            ["get", "pods", "-A", "-l", "app=flowgent-jobmanager"],
        ),
        (
            "JM-owned runtime deployments",
            ["get", "deployments", "-A", "-l", "flowgent.io/managed-by=jobmanager"],
        ),
        (
            "JM-owned runtime pods",
            ["get", "pods", "-A", "-l", "flowgent.io/managed-by=jobmanager"],
        ),
    ]
    for label, args in checks:
        data, err = _kubectl_json(args)
        if data is None:
            print(f"  [3.8] WARN: Could not query {label}: {err}")
            failures.append(f"could not query idle workload {label}: {err}")
            continue
        items = data.get("items", [])
        if items:
            found = _names(items)
            print(f"  [3.8] WARN: Idle workload {label} exist after import: {found}")
            failures.append(f"idle workload {label} exist after import: {found}")
        else:
            print(f"  [3.8] {label}: none after metadata import")


def _verify_security_flow_network_policy(failures):
    result = _pg_query(
        "SELECT definition::text FROM orh_agentflow "
        "WHERE namespace_id='default' AND agentflow_id='security-autonomy-fixer' "
        "AND del_flag=false ORDER BY version DESC LIMIT 1;"
    )
    if result.returncode != 0:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.7] WARN: Could not query security-autonomy-fixer definition: {stderr_short}")
        failures.append(f"could not query security-autonomy-fixer definition: {stderr_short}")
        return
    raw = result.stdout.strip()
    if not raw:
        print("  [3.7] WARN: security-autonomy-fixer definition not found")
        failures.append("security-autonomy-fixer definition not found")
        return
    proxy_placeholder = "${" + PROXY_ALLOWLIST_ENV + "}"
    if proxy_placeholder in raw:
        print("  [3.7] WARN: unresolved proxy allowlist placeholder found in security-autonomy-fixer definition")
        failures.append("unresolved proxy allowlist placeholder found in security-autonomy-fixer definition")
        return
    try:
        definition = json.loads(raw)
    except json.JSONDecodeError as exc:
        print(f"  [3.7] WARN: security-autonomy-fixer definition is not JSON: {exc}")
        failures.append(f"security-autonomy-fixer definition is not JSON: {exc}")
        return
    git_node = next((n for n in definition.get("nodes", []) if n.get("id") == "git-clone"), None)
    if not git_node:
        print("  [3.7] WARN: git-clone node missing from security-autonomy-fixer")
        failures.append("git-clone node missing from security-autonomy-fixer")
        return
    policy = git_node.get("network_policy") or {}
    allowed = policy.get("allowed") or []
    expected_proxy = os.environ.get(PROXY_ALLOWLIST_ENV, "github.com")
    missing = [entry for entry in ("github.com", expected_proxy) if entry not in allowed]
    if missing:
        print(f"  [3.7] WARN: git-clone network allowlist missing: {missing}")
        failures.append(f"git-clone network allowlist missing: {missing}")
        return
    print(f"  [3.7] git-clone network allowlist OK ({', '.join(allowed)})")


def run():
    failures = []

    # ── L1.1: Binary exists ─────────────────────────────────
    print("\n── L1: Binary Check ──")
    binary = None if UI_PROVISION_MODE else _find_binary()
    if UI_PROVISION_MODE:
        print("  [1.1] UI provisioning mode: console binary/import is intentionally not executed")
    elif binary:
        result = subprocess.run([binary, "--help"], capture_output=True, text=True, timeout=10)
        version_out = (result.stdout + result.stderr)[:300]
        has_console = any(kw in version_out.lower() for kw in ["console", "import", "export", "flowgent"])
        print(f"  [1.1] Binary: {binary}")
        if has_console:
            print("  [1.1] Console subcommand detected in help output")
        else:
            print("  [1.1] WARN: Console subcommand not found in help — binary may be minimal build")
    elif not UI_PROVISION_MODE:
        print("  [1.1] WARN: flowgent-core binary not found. Searched:")
        for bp in BINARY_PATHS:
            print(f"          {bp}")
        print("  [1.1] INFO: Build with 'make build:core' first, then re-run this scenario.")
        binary = None
        failures.append("flowgent-core binary not found")

    # ── L1.2: Console import subcommand ─────────────────────
    if binary:
        result = subprocess.run([binary, "console", "import", "--help"], capture_output=True, text=True, timeout=10)
        help_out = (result.stdout + result.stderr)[:500]
        print(f"  [1.2] console import --help rc={result.returncode}")
        if "import" in help_out.lower() or "usage" in help_out.lower() or "glob" in help_out.lower():
            print("  [1.2] Console import subcommand available")
        else:
            print(f"  [1.2] Output: {help_out[:200]}")

    # ── L2.1: Import config YAMLs ────────────────────────────
    print("\n── L2: Config Import ──")
    imported_count = 0
    import_summary = ""

    if UI_PROVISION_MODE:
        print("  [2.1] Validating visible UI provisioning evidence; no console import")
        try:
            with open(UI_EVIDENCE_PATH) as evidence_file:
                evidence = json.load(evidence_file)
            with open(RUN_ID_PATH) as run_file:
                run_id = run_file.read().strip()
            configured = evidence.get("configured", {})
            expected = {
                "llm_providers": MIN_LLM_PROVIDERS,
                "mcps": MIN_MCPS,
                "agents": 7,
                "runtime_skills": MIN_SKILLS,
                "flows": MIN_FLOWS,
            }
            missing = {
                key: (configured.get(key, 0), minimum)
                for key, minimum in expected.items()
                if configured.get(key, 0) < minimum
            }
            if evidence.get("provisioning_mode") != "ui" or evidence.get("run_id") != run_id or missing:
                raise AssertionError(
                    f"mode={evidence.get('provisioning_mode')} "
                    f"run_match={evidence.get('run_id') == run_id} missing={missing}"
                )
            imported_count = sum(int(value) for value in configured.values())
            print(f"  [2.2] UI evidence accepted: {configured}; run_id={run_id}")
        except Exception as exc:
            failures.append(f"UI provisioning evidence invalid: {exc}")
            print(f"  [2.2] WARN: {exc}")
    elif binary:
        cfg = _find_config()
        if not cfg:
            print("  [2.1] WARN: No flowgent config file found. Searched:")
            for cp in CONFIG_PATHS:
                print(f"          {cp}")
            print("  [2.1] INFO: Skipping import — no config to connect to PG.")
        elif not os.path.isdir(CONFIG_DIR):
            print(f"  [2.1] WARN: Config directory not found: {CONFIG_DIR}")
        else:
            print(f"  [2.1] Running: {binary} --config {cfg} console import {CONFIG_DIR}")
            env = dict(os.environ)
            result = subprocess.run(
                [binary, "--config", cfg, "console", "import", CONFIG_DIR],
                capture_output=True, text=True, timeout=60,
                env=env,
            )
            stdout = result.stdout
            stderr = result.stderr[:500]
            rc = result.returncode
            print(f"  [2.1] rc={rc}")
            if rc != 0:
                failures.append(f"console import failed with rc={rc}")
            if stdout:
                for line in stdout.splitlines():
                    if line.strip():
                        print(f"        {line.strip()}")
            if stderr:
                for line in stderr.splitlines():
                    if line.strip():
                        print(f"        stderr: {line.strip()}")
            import_summary = stdout

            # Count OK lines
            imported_count = stdout.count(" OK ")
            if imported_count > 0:
                print(f"  [2.2] Resources imported: {imported_count}")
                if imported_count >= 10:
                    print("  [2.2] Import count meets minimum (≥10)")
                else:
                    print("  [2.2] WARN: Fewer resources than expected (≥10)")
                    failures.append(f"console import reported only {imported_count} resources")
            else:
                print("  [2.2] NOTE: No 'OK' markers in output — may use bulk ImportAll format")
                # Check for bulk import summary line
                if "Imported from" in stdout:
                    print("  [2.2] Bulk import summary detected")
                else:
                    failures.append("console import produced no OK markers or bulk summary")
    elif not UI_PROVISION_MODE:
        print("  [2.1] SKIP: Binary not available, skipping import execution.")
        print("  [2.2] SKIP: No import output to analyze.")

    # ── L3: Database Verification ────────────────────────────
    print("\n── L3: Database Verification ──")

    # ── L3.1: Agents ─────────────────────────────────────────
    result = _pg_query("SELECT COUNT(*) FROM llm_agent WHERE namespace_id='default' AND del_flag=false;")
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.1] llm_agent: {count} records (min expected: {MIN_AGENTS})")
        if count >= MIN_AGENTS:
            print(f"  [3.1] Agents OK")
        else:
            print(f"  [3.1] WARN: Expected ≥{MIN_AGENTS} agents, found {count}")
            failures.append(f"expected >={MIN_AGENTS} agents, found {count}")
            # List agent names for diagnosis
            result2 = _pg_query("SELECT name FROM llm_agent WHERE namespace_id='default' AND del_flag=false ORDER BY name;")
            if result2.returncode == 0 and result2.stdout.strip():
                for line in result2.stdout.strip().splitlines():
                    print(f"        Agent: {line.strip()}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.1] WARN: Could not query llm_agent: {stderr_short}")
        failures.append(f"could not query llm_agent: {stderr_short}")

    # ── L3.2: Flows ──────────────────────────────────────────
    result = _pg_query(
        "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false "
        "AND COALESCE(NULLIF(lower(definition::jsonb->>'kind'), ''), 'flow')='flow';"
    )
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.2] orh_agentflow (flows): {count} records (min expected: {MIN_FLOWS})")
        if count >= MIN_FLOWS:
            print(f"  [3.2] Flows OK")
        else:
            print(f"  [3.2] WARN: Expected ≥{MIN_FLOWS} flows, found {count}")
            failures.append(f"expected >={MIN_FLOWS} flows, found {count}")
            result2 = _pg_query(
                "SELECT agentflow_id FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false "
                "AND COALESCE(NULLIF(lower(definition::jsonb->>'kind'), ''), 'flow')='flow' ORDER BY agentflow_id;"
            )
            if result2.returncode == 0 and result2.stdout.strip():
                for line in result2.stdout.strip().splitlines():
                    print(f"        Flow: {line.strip()}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.2] WARN: Could not query orh_agentflow: {stderr_short}")
        failures.append(f"could not query orh_agentflow: {stderr_short}")

    # ── L3.3: MCPs ───────────────────────────────────────────
    result = _pg_query("SELECT COUNT(*) FROM llm_mcp WHERE namespace_id='default' AND del_flag=false;")
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.3] llm_mcp: {count} records (min expected: {MIN_MCPS})")
        if count >= MIN_MCPS:
            print(f"  [3.3] MCPs OK")
        else:
            print(f"  [3.3] WARN: Expected ≥{MIN_MCPS} MCPs, found {count}")
            failures.append(f"expected >={MIN_MCPS} MCPs, found {count}")
            result2 = _pg_query("SELECT name FROM llm_mcp WHERE namespace_id='default' AND del_flag=false ORDER BY name;")
            if result2.returncode == 0 and result2.stdout.strip():
                for line in result2.stdout.strip().splitlines():
                    print(f"        MCP: {line.strip()}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.3] WARN: Could not query llm_mcp: {stderr_short}")
        failures.append(f"could not query llm_mcp: {stderr_short}")

    # ── L3.4: LLM Providers ──────────────────────────────────
    result = _pg_query("SELECT COUNT(*) FROM llm_providers WHERE namespace_id='default' AND del_flag=false;")
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.4] llm_providers: {count} records (min expected: {MIN_LLM_PROVIDERS})")
        if count >= MIN_LLM_PROVIDERS:
            print(f"  [3.4] LLM Providers OK")
        else:
            print(f"  [3.4] WARN: Expected ≥{MIN_LLM_PROVIDERS} LLM providers, found {count}")
            failures.append(f"expected >={MIN_LLM_PROVIDERS} LLM providers, found {count}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.4] WARN: Could not query llm_providers: {stderr_short}")
        failures.append(f"could not query llm_providers: {stderr_short}")

    # ── L3.5: Notify Channels ────────────────────────────────
    result = _pg_query("SELECT COUNT(*) FROM nfy_channel WHERE namespace_id='default' AND del_flag=false;")
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.5] nfy_channel: {count} records (min expected: {MIN_NOTIFIERS})")
        if count >= MIN_NOTIFIERS:
            print("  [3.5] Notify Channels OK")
        else:
            print(f"  [3.5] WARN: Expected ≥{MIN_NOTIFIERS} channels, found {count}")
            failures.append(f"expected >={MIN_NOTIFIERS} notify channels, found {count}")
            result2 = _pg_query("SELECT name FROM nfy_channel WHERE namespace_id='default' AND del_flag=false ORDER BY name;")
            if result2.returncode == 0 and result2.stdout.strip():
                for line in result2.stdout.strip().splitlines():
                    print(f"        Channel: {line.strip()}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.5] WARN: Could not query nfy_channel: {stderr_short}")
        failures.append(f"could not query nfy_channel: {stderr_short}")

    # ── L3.6: Skills ─────────────────────────────────────────
    result = _pg_query(
        "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false "
        "AND lower(definition::jsonb->>'kind')='skill';"
    )
    if result.returncode == 0:
        count = int(result.stdout.strip() or "0")
        print(f"  [3.6] orh_agentflow (skills): {count} records (min expected: {MIN_SKILLS})")
        if count >= MIN_SKILLS:
            print("  [3.6] Skills OK")
        else:
            print(f"  [3.6] WARN: Expected ≥{MIN_SKILLS} skills, found {count}")
            failures.append(f"expected >={MIN_SKILLS} skills, found {count}")
            result2 = _pg_query(
                "SELECT agentflow_id FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false "
                "AND lower(definition::jsonb->>'kind')='skill' ORDER BY agentflow_id;"
            )
            if result2.returncode == 0 and result2.stdout.strip():
                for line in result2.stdout.strip().splitlines():
                    print(f"        Skill: {line.strip()}")
    else:
        stderr_short = result.stderr[:200].strip()
        print(f"  [3.6] WARN: Could not query orh_agentflow for skills: {stderr_short}")
        failures.append(f"could not query orh_agentflow for skills: {stderr_short}")

    # ── L3.7: Flow spec network policy ───────────────────────
    _verify_security_flow_network_policy(failures)

    # ── L3.8: Metadata import must not allocate runtime pods ──
    if UI_PROVISION_MODE:
        print("  [3.8] UI mode already triggered a real run; idle-runtime assertion is not applicable")
    else:
        _verify_no_idle_workload_runtime(failures)

    # ── L3.9: Cross-table summary ────────────────────────────
    print("\n  ── Resource Inventory ──")
    inventory_queries = {
        "Agents":      "SELECT COUNT(*) FROM llm_agent WHERE namespace_id='default' AND del_flag=false",
        "Flows":       "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false AND COALESCE(NULLIF(lower(definition::jsonb->>'kind'), ''), 'flow')='flow'",
        "MCPs":        "SELECT COUNT(*) FROM llm_mcp WHERE namespace_id='default' AND del_flag=false",
        "LLM Providers": "SELECT COUNT(*) FROM llm_providers WHERE namespace_id='default' AND del_flag=false",
        "Channels":    "SELECT COUNT(*) FROM nfy_channel WHERE namespace_id='default' AND del_flag=false",
        "Skills":      "SELECT COUNT(*) FROM orh_agentflow WHERE namespace_id='default' AND del_flag=false AND lower(definition::jsonb->>'kind')='skill'",
    }
    all_ok = True
    for label, query in inventory_queries.items():
        result = _pg_query(query)
        if result.returncode == 0:
            count = result.stdout.strip()
            print(f"  {label:16s}: {count}")
        else:
            print(f"  {label:16s}: ERROR ({result.stderr[:100].strip()})")
            all_ok = False
            failures.append(f"inventory query failed for {label}")

    # ── Summary ────────────────────────────────────────────────
    print(f"\n  Resource provisioning & DB verification complete.")
    if UI_PROVISION_MODE:
        print(f"  Provisioning: {imported_count} resources configured through visible UI interactions.")
    elif imported_count > 0:
        print(f"  Import: {imported_count} resources imported via console.")
    else:
        print("  Import: Not executed (binary or config missing). DB verification only.")
    if all_ok:
        print("  DB: All tables reachable.")
    else:
        print("  DB: Some tables unreachable — check PG connectivity.")
    if failures:
        raise AssertionError(f"Console import verification failed: {failures}")
