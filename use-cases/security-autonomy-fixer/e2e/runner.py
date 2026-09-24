#!/usr/bin/env python3
"""Unified K8s/Docker entrypoint for the security autonomy fixer E2E."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import traceback


SHELL_ENV_FILES = ("~/.bashrc", "~/.bash_profile", "~/.wl4gshrc.sec")


class E2ERunner:
    """Class-owned operations for runner."""

    @staticmethod
    def _load_shell_environment() -> None:
        """Load exported local E2E credentials without ever printing their values."""
        files = " ".join(f'"{path}"' for path in SHELL_ENV_FILES)
        script = f"""
    set -a
    for f in {files}; do
      f="${{f/#\\~/$HOME}}"
      [ -f "$f" ] && . "$f" >/dev/null 2>&1 || true
    done
    set +a
    python3 - <<'PY'
    import json, os
    print(json.dumps(dict(os.environ), separators=(",", ":")))
    PY
    """
        try:
            completed = subprocess.run(
                ["bash", "-lc", script],
                capture_output=True,
                text=True,
                timeout=15,
                check=False,
            )
            environment = json.loads(completed.stdout.strip().splitlines()[-1])
        except Exception:
            return
        for key, value in environment.items():
            if isinstance(key, str) and isinstance(value, str):
                os.environ[key] = value

    @staticmethod
    def _deployer_name(value: str) -> str:
        """Expose one K8s mode while accepting the former name as an alias."""
        normalized = value.strip().lower()
        if normalized == "kubernetes":
            return "k8s"
        if normalized in {"k8s", "docker"}:
            return normalized
        raise argparse.ArgumentTypeError("deployer must be 'k8s' or 'docker'")

    @staticmethod
    def parse_args() -> argparse.Namespace:
        parser = argparse.ArgumentParser(
            description="Flowgent security-autonomy-fixer E2E (K8s or Docker Compose)"
        )
        parser.add_argument(
            "command",
            nargs="?",
            choices=("cleanup",),
            help="Remove only resources owned by the selected E2E deployer.",
        )
        parser.add_argument(
            "--deployer",
            type=E2ERunner._deployer_name,
            choices=("k8s", "docker"),
            default=E2ERunner._deployer_name(os.getenv("FLOWGENT_E2E_DEPLOYER", "k8s")),
            help="Deployment backend; both run the same functional verifier matrix.",
        )
        parser.add_argument("--skip-sonarqube", action="store_true")
        parser.add_argument("--skip-build", action="store_true")
        parser.add_argument("--skip-import", action="store_true")
        parser.add_argument("--skip-deploy", action="store_true")
        parser.add_argument("--no-verify", action="store_true")
        parser.add_argument(
            "--clean-after-run",
            action="store_true",
            default=False,
            help="Remove selected-backend resources after the run (default: retain).",
        )
        parser.add_argument("--cleanup", action="store_true", help="Alias for cleanup command")
        parser.add_argument("--access", action="store_true", help="Print/open manual endpoints")
        parser.add_argument("--namespace", "-n", default=config.K8S_NAMESPACE)
        parser.add_argument("--release", "-r", default=config.RELEASE_NAME)
        parser.add_argument("--timeout", type=int, default=900)
        parser.add_argument(
            "--scenario",
            "-s",
            action="append",
            default=[],
            help="Verifier id or comma-separated ids, for example -s 01,14.",
        )
        parser.add_argument("--list", "-l", action="store_true")
        parser.add_argument("--api")
        parser.add_argument("--pg")
        return parser.parse_args()

    @staticmethod
    def selected_scenarios(raw_values: list[str]) -> list[str]:
        if not raw_values:
            return list(DEFAULT_SCENARIOS)
        selected: list[str] = []
        for raw_value in raw_values:
            for scenario_id in raw_value.split(","):
                scenario_id = scenario_id.strip().zfill(2)
                if scenario_id not in SCENARIOS:
                    raise ValueError(f"unknown verifier id: {scenario_id}")
                if scenario_id not in selected:
                    selected.append(scenario_id)
        return selected

    @staticmethod
    def context_from_args(args: argparse.Namespace, round_number: int = 1) -> RunContext:
        os.environ["FLOWGENT_E2E_DEPLOYER"] = args.deployer
        return RunContext(
            round_number=round_number,
            clean=True,
            timeout_seconds=args.timeout,
            build_images=not args.skip_build,
            namespace=args.namespace,
            release=args.release,
            deployer=args.deployer,
        )

    @staticmethod
    def cleanup_deployment(context: RunContext) -> bool:
        try:
            E2EDeployerFactory.create(context).cleanup()
        except Exception:
            traceback.print_exc()
            return False
        return True

    @staticmethod
    def print_access(context: RunContext) -> int:
        if context.deployer == "docker":
            print(
                "Flowgent Web and AuthGuard Gateway: "
                f"http://127.0.0.1:{config.LOCAL_GATEWAY_PORT}"
            )
            print(f"Jaeger: http://127.0.0.1:{config.LOCAL_JAEGER_PORT}")
            return 0
        from deploy.base.kubernetes import KubernetesManualAccess

        KubernetesManualAccess.start(context)
        return 0

    @staticmethod
    def _archive_reports() -> None:
        archive = E2EReportWriter.archive()
        if archive:
            print(f"Archived previous reports: {archive}")

    @staticmethod
    def _write_run_report(
        context: RunContext,
        *,
        passed: bool,
        duration_seconds: float,
        title: str,
        error: str | None = None,
        details: list[str] | None = None,
    ) -> None:
        result = VerificationResult(
            scenario_id="00",
            title=title,
            passed=passed,
            duration_seconds=duration_seconds,
            error=error,
            details=details or [],
        )
        E2EReportWriter.write_round(context.round_number, result)
        E2EReportWriter.write_summary([[result]])

    @staticmethod
    def main() -> int:
        args = E2ERunner.parse_args()
        if args.list:
            for scenario_id, (title, _) in SCENARIOS.items():
                print(f"{scenario_id}  {title}")
            return 0
        if args.timeout < 1:
            print("--timeout must be at least 1", file=sys.stderr)
            return 2
        try:
            scenario_ids = E2ERunner.selected_scenarios(args.scenario)
        except ValueError as failure:
            print(failure, file=sys.stderr)
            return 2

        context = E2ERunner.context_from_args(args)
        cleanup_requested = args.command == "cleanup" or args.cleanup
        if cleanup_requested:
            if args.clean_after_run:
                print("cleanup and --clean-after-run cannot be combined", file=sys.stderr)
                return 2
            return 0 if E2ERunner.cleanup_deployment(context) else 1
        if args.access:
            return E2ERunner.print_access(context)
        if args.api:
            config.K8S_APISERVER_URL = args.api
        if args.pg:
            config.E2EConfiguration.apply_postgres_override(args.pg)

        exit_code = 1
        started = time.monotonic()
        results: list[VerificationResult] = []
        reports_started = False
        deployer = None
        try:
            E2ERunner._archive_reports()
            reports_started = True
            deployer = E2EDeployerFactory.create(context)
            deployer.prepare(
                skip_sonarqube=args.skip_sonarqube,
                skip_build=args.skip_build,
                skip_deploy=args.skip_deploy,
                skip_import=args.skip_import,
            )
            if args.no_verify:
                exit_code = 0
                E2ERunner._write_run_report(
                    context,
                    passed=True,
                    duration_seconds=time.monotonic() - started,
                    title="E2E Deployment — Verification Skipped",
                    details=["Deployment completed successfully; verifier matrix was skipped by --no-verify."],
                )
            else:
                # Keep metadata-only CLI operations (--help/--list) independent
                # from the third-party packages required by real verifiers.
                from common.project import VerificationRunner

                passed, results = VerificationRunner.run_matrix(
                    context,
                    scenario_ids,
                    archive_existing=False,
                )
                exit_code = 0 if passed else 1
                if passed and not args.clean_after_run:
                    print(
                        f"Retained {context.deployer} deployment for manual use: "
                        f"release={context.release} namespace={context.namespace}"
                    )
        except KeyboardInterrupt:
            exit_code = 130
            if reports_started:
                E2ERunner._write_run_report(
                    context,
                    passed=False,
                    duration_seconds=time.monotonic() - started,
                    title="E2E Execution — Interrupted",
                    error="E2E execution interrupted by user.",
                )
        except Exception:
            traceback.print_exc()
            exit_code = 1
            if reports_started:
                E2ERunner._write_run_report(
                    context,
                    passed=False,
                    duration_seconds=time.monotonic() - started,
                    title="E2E Deployment — Failed Before Verification",
                    error=traceback.format_exc(),
                )
        finally:
            if deployer is not None and args.clean_after_run and not E2ERunner.cleanup_deployment(context) and exit_code == 0:
                exit_code = 1
                if reports_started:
                    cleanup_failure = VerificationResult(
                        scenario_id="00",
                        title="E2E Cleanup",
                        passed=False,
                        duration_seconds=time.monotonic() - started,
                        details=["The selected E2E deployment could not be cleaned up."],
                    )
                    E2EReportWriter.write_round(context.round_number, cleanup_failure)
                    E2EReportWriter.write_summary([results + [cleanup_failure]])
        return exit_code


E2ERunner._load_shell_environment()
os.environ.setdefault("FLOWGENT_E2E_PROXY_ALLOWLIST_ENTRY", "github.com")

from common import config
from common.config import DEFAULT_SCENARIOS, SCENARIOS
from deploy import E2EDeployerFactory
from common.model import RunContext, VerificationResult
from common.report import E2EReportWriter

if __name__ == "__main__":
    raise SystemExit(E2ERunner.main())
