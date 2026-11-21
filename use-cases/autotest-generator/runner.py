"""
Flowgent E2E Runner — pipeline orchestration + scenario verification.

Orchestrates:
  1. SonarQube PG reset (docker compose)
  2. Build flowgent-core binary (make build:core)
  3. Import config YAMLs into Flowgent DB (console import)
  4. Flowgent Helm deploy (helm install/upgrade + wait for pods)
  5. Verification suite (scenario-based checks)

Usage:
  python3 runner.py                             # full pipeline + verify all
  python3 runner.py --skip-build                # skip binary build
  python3 runner.py --no-verify                 # pipeline only, skip verification
  python3 runner.py --skip-deploy --no-verify   # setup only
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy
                                                # verify only (assumes infra is up)
  python3 runner.py --skip-sonarqube --skip-build --skip-import --skip-deploy -s 11
                                                # verify single scenario
  python3 runner.py -l                          # list available scenarios
  python3 runner.py --api http://10.0.0.1:9999 --pg postgres://u:p@h/db
                                                # override config

Reports are written to reports/ after every verification run.
"""

import argparse
import contextlib
import importlib
import io
import os
import sys
import time
import traceback

from util.process import E2EProcessHelper

E2EProcessHelper.load_shell_environment()

from util.constant import E2EOtherDefinitions as config

E2E_DIR = os.path.dirname(os.path.abspath(__file__))
REPORTS_DIR = os.path.join(E2E_DIR, "reports")
SCENARIOS = config.SCENARIOS


class E2ERunner:
    @staticmethod
    def _step_sonarqube():
        from verifier.infra._sonarqube import SonarQubeDeployment

        compose_up = SonarQubeDeployment.compose_up
        wait_for_sonarqube = SonarQubeDeployment.wait_for_sonarqube
        verify_credentials = SonarQubeDeployment.verify_credentials
        if not compose_up():
            return False
        if not wait_for_sonarqube():
            return False
        verify_credentials()
        return True

    @staticmethod
    def _step_build():
        from util.constant import E2EOtherDefinitions as config

        PROJECT_ROOT = config.PROJECT_ROOT
        CONSOLE_BIN = config.CONSOLE_BIN
        from util.process import E2EProcessHelper

        run_cmd = E2EProcessHelper.legacy
        print("\n-- Step: Build flowgent-core --")
        from util.images import E2EImagesDefinitions

        E2EImagesDefinitions.ensure_build_images()
        for target in ("build:core", "build:image:core", "build:image:web"):
            rc, _ = run_cmd(
                ["make", "-C", PROJECT_ROOT, target],
                timeout=1200,
                env=E2EImagesDefinitions.build_environment(
                    container=target != "build:core"
                ),
            )
            if rc != 0:
                print(f"  ERROR: Build failed: {target}")
                return False
        if os.path.isfile(CONSOLE_BIN):
            print(f"  Binary: {CONSOLE_BIN}")
            return True
        print(f"  ERROR: Binary not found after build: {CONSOLE_BIN}")
        return False

    @staticmethod
    def _step_import():
        from util.constant import E2EOtherDefinitions as config

        PROJECT_ROOT = config.PROJECT_ROOT
        CONSOLE_BIN = config.CONSOLE_BIN
        CONSOLE_CFG = config.CONSOLE_CFG
        CONFIG_DIR = config.CONFIG_DIR
        from util.process import E2EProcessHelper

        run_cmd = E2EProcessHelper.legacy
        print("\n-- Step: Import Config YAMLs --")
        if not os.path.isfile(CONSOLE_BIN):
            print(f"  ERROR: Binary not found: {CONSOLE_BIN}")
            print(f"  Build first: make -C {PROJECT_ROOT} build:core")
            return False
        if not os.path.isfile(CONSOLE_CFG):
            print(f"  ERROR: Config file not found: {CONSOLE_CFG}")
            return False
        if not os.path.isdir(CONFIG_DIR):
            print(f"  ERROR: Config dir not found: {CONFIG_DIR}")
            return False
        print(f"  Binary:     {CONSOLE_BIN}")
        print(f"  Config:     {CONSOLE_CFG}")
        print(f"  Config dir: {CONFIG_DIR}")
        rc, _ = run_cmd(
            [CONSOLE_BIN, "--config", CONSOLE_CFG, "console", "import", CONFIG_DIR],
            timeout=60,
        )
        if rc != 0:
            print("  ERROR: Console import returned non-zero exit code")
            return False
        print("  Config import complete.")
        return True

    @staticmethod
    def _step_deploy(namespace, release):
        from verifier.infra._application import FlowgentDeployment

        helm_install_or_upgrade = FlowgentDeployment.helm_install_or_upgrade
        wait_for_pods = FlowgentDeployment.wait_for_pods
        flowgent_health = FlowgentDeployment.health_check
        if not helm_install_or_upgrade(release, namespace):
            return False
        if not wait_for_pods(namespace, release, 300):
            return False
        flowgent_health(namespace, release)
        return True

    @staticmethod
    def run_pipeline(args):
        ok = True
        if not args.skip_sonarqube:
            if not E2ERunner._step_sonarqube():
                ok = False
        if not args.skip_build:
            if not E2ERunner._step_build():
                ok = False
        if not args.skip_import:
            if not E2ERunner._step_import():
                ok = False
        if not args.skip_deploy:
            if not E2ERunner._step_deploy(args.namespace, args.release):
                ok = False
        return ok

    @staticmethod
    def _archive_old_reports():
        from util.report import E2EReportHelper

        E2EReportHelper.archive()

    @staticmethod
    def _write_report(num, name, passed, elapsed, output, error=None):
        from util.report import E2EReportHelper, VerificationResult

        result = VerificationResult(
            num, name, passed, elapsed, output=output.getvalue(), error=error
        )
        return E2EReportHelper.write_round(1, result)

    @staticmethod
    def _write_summary(results, total_elapsed):
        from util.report import E2EReportHelper, VerificationResult

        rows = [
            VerificationResult(
                num,
                result["name"],
                result["passed"],
                result["elapsed"],
                error=result["error"],
            )
            for num, result in results.items()
        ]
        return E2EReportHelper.write_summary([rows])

    @staticmethod
    def _run_scenario(num, name, module_path, args):
        print(f"\n{'=' * 60}")
        print(f"  Scenario {num}: {name}")
        print(f"{'=' * 60}")
        output = io.StringIO()
        start = time.time()
        passed = False
        error_msg = None
        try:
            mod = importlib.import_module(module_path)

            class Tee(io.StringIO):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, **kwargs)
                    self.terminal = sys.stdout

                def write(self, s):
                    self.terminal.write(s)
                    super().write(s)

                def flush(self):
                    self.terminal.flush()
                    super().flush()

            tee = Tee()
            with contextlib.redirect_stdout(tee):
                from verifier.context import BaseVerifier, VerificationContext

                candidates = [
                    value
                    for value in vars(mod).values()
                    if isinstance(value, type)
                    and value is not BaseVerifier
                    and value.__module__ == mod.__name__
                    and issubclass(value, BaseVerifier)
                ]
                if len(candidates) != 1:
                    raise TypeError(
                        f"{module_path} must define exactly one scenario Verifier"
                    )
                verification = candidates[0](
                    VerificationContext(
                        namespace=args.namespace,
                        release=args.release,
                        timeout_seconds=args.timeout,
                    )
                ).run()
                if verification.scenario_id != num or verification.title != name:
                    raise AssertionError("Scenario metadata differs from the registry")
                for detail in verification.details:
                    print(detail)
                if not verification.passed:
                    raise AssertionError(
                        verification.error or "Scenario validation failed"
                    )
            output = io.StringIO(tee.getvalue())
            elapsed = time.time() - start
            print(f"  PASS ({elapsed:.1f}s)")
            passed = True
        except Exception as e:
            if "tee" in locals():
                output = io.StringIO(tee.getvalue())
            elapsed = time.time() - start
            error_msg = f"{e}\n{traceback.format_exc()}"
            print(f"  FAIL ({elapsed:.1f}s): {e}")
            traceback.print_exc()
        return {
            "num": num,
            "name": name,
            "passed": passed,
            "elapsed": elapsed,
            "output": output,
            "error": error_msg,
        }

    @staticmethod
    def run_verification(args):
        if not SCENARIOS:
            print(
                "  ERROR: an empty scenario matrix is not successful E2E verification."
            )
            return False
        E2ERunner._archive_old_reports()
        suite_start = time.time()
        results = {}
        if args.scenario:
            num = args.scenario
            if num not in SCENARIOS:
                print(f"Unknown scenario: {num}")
                return False
            name, mod = SCENARIOS[num]
            r = E2ERunner._run_scenario(num, name, mod, args)
            results[num] = r
            E2ERunner._write_report(
                num, name, r["passed"], r["elapsed"], r["output"], r["error"]
            )
        else:
            for num, (name, mod) in SCENARIOS.items():
                r = E2ERunner._run_scenario(num, name, mod, args)
                results[num] = r
                E2ERunner._write_report(
                    num, name, r["passed"], r["elapsed"], r["output"], r["error"]
                )
        total_elapsed = time.time() - suite_start
        passed = sum((1 for v in results.values() if v["passed"]))
        total = len(results)
        summary_path = E2ERunner._write_summary(results, total_elapsed)
        print(f"\n{'=' * 60}")
        print(f"  Results: {passed}/{total} passed ({total_elapsed:.1f}s)")
        print(f"  Reports: {summary_path}")
        print(f"{'=' * 60}")
        return passed == total

    @staticmethod
    def main():
        parser = argparse.ArgumentParser(
            description="Flowgent E2E Runner — pipeline + verification"
        )
        parser.add_argument(
            "--skip-sonarqube", action="store_true", help="Skip SonarQube PG reset"
        )
        parser.add_argument(
            "--skip-build", action="store_true", help="Skip binary build"
        )
        parser.add_argument(
            "--skip-import", action="store_true", help="Skip config YAML import"
        )
        parser.add_argument(
            "--skip-deploy", action="store_true", help="Skip Helm deploy"
        )
        parser.add_argument(
            "--no-verify", action="store_true", help="Skip verification suite"
        )
        parser.add_argument("--namespace", "-n", default=config.K8S_NAMESPACE)
        parser.add_argument("--release", "-r", default="flowgent")
        parser.add_argument(
            "--scenario", "-s", help="Run specific scenario (e.g. 11, 31)"
        )
        parser.add_argument(
            "--list", "-l", action="store_true", help="List available scenarios"
        )
        parser.add_argument(
            "--api", help=f"K8S API server URL (default: {config.K8S_APISERVER_URL})"
        )
        parser.add_argument("--pg", help=f"PG DSN (default: {config.postgres_dsn()})")
        parser.add_argument("--timeout", type=int, default=300)
        args = parser.parse_args()
        if args.list:
            if SCENARIOS:
                for k, (name, _) in SCENARIOS.items():
                    print(f"  {k}: {name}")
            else:
                print("  No scenarios defined yet.")
            return
        if args.api:
            config.K8S_APISERVER_URL = args.api
        if args.pg:
            config.apply_postgres_override(args.pg)
        print("=" * 60)
        print("  Flowgent E2E Runner [autotest-generator]")
        print(f"  API:  {config.K8S_APISERVER_URL}")
        print(f"  PG:   {config.PG_HOST}:{config.PG_PORT}/{config.PG_DATABASE}")
        print("=" * 60)
        all_steps_skipped = (
            args.skip_sonarqube
            and args.skip_build
            and args.skip_import
            and args.skip_deploy
        )
        if not SCENARIOS:
            print(
                "ERROR: no registered scenarios; E2E cannot pass an empty matrix.",
                file=sys.stderr,
            )
            raise SystemExit(1)
        if args.timeout < 1:
            print("--timeout must be positive", file=sys.stderr)
            raise SystemExit(2)
        if not all_steps_skipped:
            E2ERunner._archive_old_reports()
            title, module = SCENARIOS["01"]
            preflight = E2ERunner._run_scenario("01", title, module, args)
            E2ERunner._write_report(
                "01",
                title,
                preflight["passed"],
                preflight["elapsed"],
                preflight["output"],
                preflight["error"],
            )
            if not preflight["passed"]:
                E2ERunner._write_summary({"01": preflight}, preflight["elapsed"])
                raise SystemExit(1)
        if all_steps_skipped:
            print("  All pipeline steps skipped — verify-only mode.")
        elif not all_steps_skipped:
            ok = E2ERunner.run_pipeline(args)
            print(f"\n{'=' * 60}")
            if ok:
                print("  Pipeline complete — all steps OK.")
            else:
                print("  Pipeline finished with errors — check output above.")
                print("  Verification cannot pass after pipeline errors.")
                sys.exit(1)
            print(f"{'=' * 60}")
        if args.no_verify:
            print("\n  Verification skipped (--no-verify).")
            sys.exit(0)
        all_passed = E2ERunner.run_verification(args)
        sys.exit(0 if all_passed else 1)


if __name__ == "__main__":
    E2ERunner.main()
