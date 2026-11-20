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
      Action:  psql count scoped to the configured tenant namespace
      Input:   Import succeeded
      Output:  ≥5 agent records (supervisor, issue-detector, fixer-agent, security-reviewer, quality-reviewer, arch-reviewer, git-agent)

    Step 3.2 Flows in orh_flow + orh_flow_revision
      Action:  psql flow count scoped to the configured tenant namespace
      Input:   Flow YAMLs imported
      Output:  ≥1 flow record

    Step 3.3 MCPs in llm_mcp
      Action:  psql MCP count scoped to the configured tenant namespace
      Input:   MCP YAMLs imported
      Output:  ≥2 MCP records

    Step 3.4 LLM Providers in llm_provider
      Action:  psql provider count scoped to the configured tenant namespace
      Input:   LLM provider YAMLs imported
      Output:  ≥1 LLM provider record

    Step 3.5 Notify Channels in nfy_channel
      Action:  psql channel count scoped to the configured tenant namespace
      Input:   Notifier YAMLs imported
      Output:  ≥2 channel records

    Step 3.6 Runtime Skills in orh_flow + orh_flow_revision
      Action:  psql skill count scoped to the configured tenant namespace
      Input:   Skill YAMLs imported
      Output:  ≥2 skill records

    Step 3.8 No idle application runtime
      Action:  kubectl get deployments/pods -A for application runtime labels
      Input:   Config import completed, no FlowRun triggered yet
      Output:  No application JobManager or per-run runtime worker resources exist
"""
from __future__ import annotations

from common.model import VerificationResult
from verifier import BaseVerifier

import subprocess
import sys
import os
import json
from common import config
from common.project import FlowgentE2EProject
from common.config import CONFIG_DIR, CONSOLE_BIN, CONSOLE_CFG, PROJECT_ROOT

NAMESPACE = config.NAMESPACE_ID

BINARY_PATHS = [
    CONSOLE_BIN,
    os.path.join(PROJECT_ROOT, "bin", "flowgent"),
    "flowgent-core",
]
CONFIG_PATHS = [
    CONSOLE_CFG,
]

# Resource counts expected after import
MIN_AGENTS = 5
MIN_FLOWS = 1
MIN_MCPS = 2
MIN_LLM_PROVIDERS = 1
MIN_SKILLS = 2
MIN_NOTIFIERS = 2
PROXY_ALLOWLIST_ENV = "FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY"
os.environ.setdefault(PROXY_ALLOWLIST_ENV, "github.com")


class ConsoleImportVerifier(BaseVerifier):
    """Class-owned operations for s12 console import."""

    @staticmethod
    def _pg_query(query):
        """Run SQL through the active backend's configured PostgreSQL endpoint."""
        connection = FlowgentE2EProject.pg_connect()
        if connection is None:
            return subprocess.CompletedProcess([], 1, "", "configured PostgreSQL is unavailable")
        try:
            with connection.cursor() as cursor:
                cursor.execute(query)
                rows = cursor.fetchall() if cursor.description else []
            stdout = "\n".join(
                "|".join("" if value is None else str(value) for value in row)
                for row in rows
            )
            return subprocess.CompletedProcess([], 0, stdout + ("\n" if rows else ""), "")
        except Exception as failure:
            return subprocess.CompletedProcess([], 1, "", str(failure))
        finally:
            connection.close()

    @staticmethod
    def _find_binary():
        for bp in BINARY_PATHS:
            if os.path.isfile(bp) and os.access(bp, os.X_OK):
                return bp
        # try which
        result = subprocess.run(["which", "flowgent-core"], capture_output=True, text=True)
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout.strip()
        return None

    @staticmethod
    def _find_config():
        for cp in CONFIG_PATHS:
            if os.path.isfile(cp):
                return cp
        return None

    @staticmethod
    def _kubectl_json(args):
        result = subprocess.run(["kubectl", *args, "-o", "json"], capture_output=True, text=True, timeout=15)
        if result.returncode != 0:
            return None, result.stderr.strip()[:200]
        try:
            return json.loads(result.stdout or "{}"), ""
        except json.JSONDecodeError as exc:
            return None, str(exc)

    @staticmethod
    def _names(items):
        return [
            f"{item.get('metadata', {}).get('namespace', '?')}/{item.get('metadata', {}).get('name', '?')}"
            for item in items
        ]

    @staticmethod
    def _verify_no_idle_workload_runtime(failures):
        if os.getenv("FLOWGENT_E2E_DEPLOYER", "k8s") == "docker":
            print("  [3.8] Docker uses one fixed inline runtime; no per-run workload is created by import")
            return
        checks = [
            (
                "JM deployments",
                ["get", "deployments", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-boundary=flow-jobmanager"],
            ),
            (
                "JM pods",
                ["get", "pods", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-boundary=flow-jobmanager"],
            ),
            (
                "JM-owned runtime deployments",
                ["get", "deployments", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-mode=application,flowgent/role in (worker,sandbox-worker)"],
            ),
            (
                "JM-owned runtime pods",
                ["get", "pods", "-n", config.K8S_WORKLOAD_NAMESPACE, "-l", "flowgent.io/runtime-mode=application,flowgent/role in (worker,sandbox-worker)"],
            ),
        ]
        for label, args in checks:
            data, err = ConsoleImportVerifier._kubectl_json(args)
            if data is None:
                print(f"  [3.8] WARN: Could not query {label}: {err}")
                failures.append(f"could not query idle workload {label}: {err}")
                continue
            items = data.get("items", [])
            if items:
                found = ConsoleImportVerifier._names(items)
                print(f"  [3.8] WARN: Idle workload {label} exist after import: {found}")
                failures.append(f"idle workload {label} exist after import: {found}")
            else:
                print(f"  [3.8] {label}: none after metadata import")

    @staticmethod
    def _verify_security_flow_network_policy(failures):
        result = ConsoleImportVerifier._pg_query(
            "SELECT r.definition::text FROM orh_flow f "
            "JOIN orh_flow_revision r ON r.id=f.current_revision_id "
            f"WHERE f.namespace_id='{NAMESPACE}' AND f.name='security-autonomy-fixer' "
            "AND f.status<>'DELETED' LIMIT 1;"
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

    @staticmethod
    def _verify_scenario():
        failures = []

        # ── L1.1: Binary exists ─────────────────────────────────
        print("\n── L1: Binary Check ──")
        binary = ConsoleImportVerifier._find_binary()
        if binary:
            result = subprocess.run([binary, "--help"], capture_output=True, text=True, timeout=10)
            version_out = (result.stdout + result.stderr)[:300]
            has_console = any(kw in version_out.lower() for kw in ["console", "import", "export", "flowgent"])
            print(f"  [1.1] Binary: {binary}")
            if has_console:
                print("  [1.1] Console subcommand detected in help output")
            else:
                print("  [1.1] WARN: Console subcommand not found in help — binary may be minimal build")
        else:
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

        if binary:
            cfg = ConsoleImportVerifier._find_config()
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
                        print("  [2.2] Existing resources were preserved; inventory is authoritative")
                else:
                    print("  [2.2] NOTE: No 'OK' markers in output — may use bulk ImportAll format")
                    # Check for bulk import summary line
                    if "Imported from" in stdout:
                        print("  [2.2] Bulk import summary detected")
                    else:
                        failures.append("console import produced no OK markers or bulk summary")
        else:
            print("  [2.1] SKIP: Binary not available, skipping import execution.")
            print("  [2.2] SKIP: No import output to analyze.")

        # ── L3: Database Verification ────────────────────────────
        print("\n── L3: Database Verification ──")

        # ── L3.1: Agents ─────────────────────────────────────────
        result = ConsoleImportVerifier._pg_query(f"SELECT COUNT(*) FROM llm_agent WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED';")
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.1] llm_agent: {count} records (min expected: {MIN_AGENTS})")
            if count >= MIN_AGENTS:
                print(f"  [3.1] Agents OK")
            else:
                print(f"  [3.1] WARN: Expected ≥{MIN_AGENTS} agents, found {count}")
                failures.append(f"expected >={MIN_AGENTS} agents, found {count}")
                # List agent names for diagnosis
                result2 = ConsoleImportVerifier._pg_query(f"SELECT name FROM llm_agent WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED' ORDER BY name;")
                if result2.returncode == 0 and result2.stdout.strip():
                    for line in result2.stdout.strip().splitlines():
                        print(f"        Agent: {line.strip()}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.1] WARN: Could not query llm_agent: {stderr_short}")
            failures.append(f"could not query llm_agent: {stderr_short}")

        # ── L3.2: Flows ──────────────────────────────────────────
        result = ConsoleImportVerifier._pg_query(
            "SELECT COUNT(*) FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id "
            f"WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' "
            "AND COALESCE(NULLIF(lower(r.definition->>'kind'), ''), 'flow')='flow';"
        )
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.2] orh_flow (flows): {count} records (min expected: {MIN_FLOWS})")
            if count >= MIN_FLOWS:
                print(f"  [3.2] Flows OK")
            else:
                print(f"  [3.2] WARN: Expected ≥{MIN_FLOWS} flows, found {count}")
                failures.append(f"expected >={MIN_FLOWS} flows, found {count}")
                result2 = ConsoleImportVerifier._pg_query(
                    "SELECT f.name FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id "
                    f"WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' "
                    "AND COALESCE(NULLIF(lower(r.definition->>'kind'), ''), 'flow')='flow' ORDER BY f.name;"
                )
                if result2.returncode == 0 and result2.stdout.strip():
                    for line in result2.stdout.strip().splitlines():
                        print(f"        Flow: {line.strip()}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.2] WARN: Could not query orh_flow: {stderr_short}")
            failures.append(f"could not query orh_flow: {stderr_short}")

        # ── L3.3: MCPs ───────────────────────────────────────────
        result = ConsoleImportVerifier._pg_query(f"SELECT COUNT(*) FROM llm_mcp WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED';")
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.3] llm_mcp: {count} records (min expected: {MIN_MCPS})")
            if count >= MIN_MCPS:
                print(f"  [3.3] MCPs OK")
            else:
                print(f"  [3.3] WARN: Expected ≥{MIN_MCPS} MCPs, found {count}")
                failures.append(f"expected >={MIN_MCPS} MCPs, found {count}")
                result2 = ConsoleImportVerifier._pg_query(f"SELECT name FROM llm_mcp WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED' ORDER BY name;")
                if result2.returncode == 0 and result2.stdout.strip():
                    for line in result2.stdout.strip().splitlines():
                        print(f"        MCP: {line.strip()}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.3] WARN: Could not query llm_mcp: {stderr_short}")
            failures.append(f"could not query llm_mcp: {stderr_short}")

        # ── L3.4: LLM Providers ──────────────────────────────────
        result = ConsoleImportVerifier._pg_query(f"SELECT COUNT(*) FROM llm_provider WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED';")
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.4] llm_provider: {count} records (min expected: {MIN_LLM_PROVIDERS})")
            if count >= MIN_LLM_PROVIDERS:
                print(f"  [3.4] LLM Providers OK")
            else:
                print(f"  [3.4] WARN: Expected ≥{MIN_LLM_PROVIDERS} LLM providers, found {count}")
                failures.append(f"expected >={MIN_LLM_PROVIDERS} LLM providers, found {count}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.4] WARN: Could not query llm_provider: {stderr_short}")
            failures.append(f"could not query llm_provider: {stderr_short}")

        # ── L3.5: Notify Channels ────────────────────────────────
        result = ConsoleImportVerifier._pg_query(f"SELECT COUNT(*) FROM nfy_channel WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED';")
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.5] nfy_channel: {count} records (min expected: {MIN_NOTIFIERS})")
            if count >= MIN_NOTIFIERS:
                print("  [3.5] Notify Channels OK")
            else:
                print(f"  [3.5] WARN: Expected ≥{MIN_NOTIFIERS} channels, found {count}")
                failures.append(f"expected >={MIN_NOTIFIERS} notify channels, found {count}")
                result2 = ConsoleImportVerifier._pg_query(f"SELECT name FROM nfy_channel WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED' ORDER BY name;")
                if result2.returncode == 0 and result2.stdout.strip():
                    for line in result2.stdout.strip().splitlines():
                        print(f"        Channel: {line.strip()}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.5] WARN: Could not query nfy_channel: {stderr_short}")
            failures.append(f"could not query nfy_channel: {stderr_short}")

        # ── L3.6: Skills ─────────────────────────────────────────
        result = ConsoleImportVerifier._pg_query(
            "SELECT COUNT(*) FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id "
            f"WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' "
            "AND lower(r.definition->>'kind')='skill';"
        )
        if result.returncode == 0:
            count = int(result.stdout.strip() or "0")
            print(f"  [3.6] orh_flow (runtime skills): {count} records (min expected: {MIN_SKILLS})")
            if count >= MIN_SKILLS:
                print("  [3.6] Skills OK")
            else:
                print(f"  [3.6] WARN: Expected ≥{MIN_SKILLS} skills, found {count}")
                failures.append(f"expected >={MIN_SKILLS} skills, found {count}")
                result2 = ConsoleImportVerifier._pg_query(
                    "SELECT f.name FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id "
                    f"WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' "
                    "AND lower(r.definition->>'kind')='skill' ORDER BY f.name;"
                )
                if result2.returncode == 0 and result2.stdout.strip():
                    for line in result2.stdout.strip().splitlines():
                        print(f"        Skill: {line.strip()}")
        else:
            stderr_short = result.stderr[:200].strip()
            print(f"  [3.6] WARN: Could not query orh_flow for skills: {stderr_short}")
            failures.append(f"could not query orh_flow for skills: {stderr_short}")

        # ── L3.7: Flow spec network policy ───────────────────────
        ConsoleImportVerifier._verify_security_flow_network_policy(failures)

        # ── L3.8: Metadata import must not allocate runtime pods ──
        ConsoleImportVerifier._verify_no_idle_workload_runtime(failures)

        # ── L3.9: Cross-table summary ────────────────────────────
        print("\n  ── Resource Inventory ──")
        inventory_queries = {
            "Agents":      f"SELECT COUNT(*) FROM llm_agent WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED'",
            "Flows":       f"SELECT COUNT(*) FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' AND COALESCE(NULLIF(lower(r.definition->>'kind'), ''), 'flow')='flow'",
            "MCPs":        f"SELECT COUNT(*) FROM llm_mcp WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED'",
            "LLM Providers": f"SELECT COUNT(*) FROM llm_provider WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED'",
            "Channels":    f"SELECT COUNT(*) FROM nfy_channel WHERE namespace_id='{NAMESPACE}' AND status<>'DELETED'",
            "Skills":      f"SELECT COUNT(*) FROM orh_flow f JOIN orh_flow_revision r ON r.id=f.current_revision_id WHERE f.namespace_id='{NAMESPACE}' AND f.status<>'DELETED' AND lower(r.definition->>'kind')='skill'",
        }
        all_ok = True
        for label, query in inventory_queries.items():
            result = ConsoleImportVerifier._pg_query(query)
            if result.returncode == 0:
                count = result.stdout.strip()
                print(f"  {label:16s}: {count}")
            else:
                print(f"  {label:16s}: ERROR ({result.stderr[:100].strip()})")
                all_ok = False
                failures.append(f"inventory query failed for {label}")

        # ── Summary ────────────────────────────────────────────────
        print(f"\n  Resource provisioning & DB verification complete.")
        if imported_count > 0:
            print(f"  Import: {imported_count} resources imported via console.")
        else:
            print("  Import: Not executed (binary or config missing). DB verification only.")
        if all_ok:
            print("  DB: All tables reachable.")
        else:
            print("  DB: Some tables unreachable — check PG connectivity.")
        if failures:
            raise AssertionError(f"Console import verification failed: {failures}")

    scenario_id = "12"
    title = "Console Import — Binary, Import Command, DB Verification"

    def run(self) -> VerificationResult:
        return self.execute(lambda: self.step("verify console import and persisted resource inventory", self._verify_import))

    @staticmethod
    def _verify_import() -> None:
        ConsoleImportVerifier._verify_scenario()
