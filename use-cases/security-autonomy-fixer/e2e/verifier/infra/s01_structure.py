"""Guard the AuthGuard-aligned E2E layering and dual-deployer parity."""

from __future__ import annotations

import ast
from pathlib import Path

import yaml

from common.config import E2E_DIR, SCENARIOS
from common.model import VerificationResult
from verifier import BaseVerifier


class E2EStructureVerifier(BaseVerifier):
    scenario_id = "01"
    title = "E2E Structure — Layered Deployment and Verifier Contracts"

    def run(self) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        self.step("verify shared deployer abstraction and both backends", self._verify_deployers)
        self.step("verify direct BaseVerifier scenario classes", self._verify_verifiers)
        self.step("verify Docker and K8s functional matrix parity", self._verify_parity)
        self.step("verify same-origin Hosted Login and sign-out routing", self._verify_gateway_browser)
        self.step("verify K8s image-runtime discovery contract", self._verify_image_importer)
        self.step("verify obsolete procedural modules are absent", self._verify_forbidden_paths)

    def _verify_deployers(self) -> None:
        required = (
            "deploy/__init__.py",
            "verifier/__init__.py",
            "deploy/base/kubernetes.py",
            "deploy/base/docker.py",
            "deploy/authguard/__init__.py",
            "deploy/flowgent/__init__.py",
            "deploy/mocksvc-github-service/app/server.py",
            "deploy/mocksvc-notification-service/app/server.py",
            "deploy/sonarqube/__init__.py",
            "deploy/docker/compose.yml",
            "config/envoy/envoy.yaml",
            "verifier/web/browser.py",
            "verifier/web/s41_flow_crud.py",
            "verifier/web/s42_agent_crud.py",
            "verifier/web/s43_skill_crud.py",
            "verifier/web/s44_integrations_crud.py",
            "verifier/web/s45_knowledge_notifications.py",
            "verifier/web/s46_hosted_login.py",
        )
        for relative in required:
            if not (E2E_DIR / relative).is_file():
                raise AssertionError(f"missing deployer layer: {relative}")
        fixture = self._class(
            self._tree(E2E_DIR / "common/agentflow.py"),
            "SecurityAutonomyFixture",
        )
        fixture_api = {
            node.name for node in fixture.body if isinstance(node, ast.FunctionDef)
        }
        if "seed_agents_and_mcps" not in fixture_api:
            raise AssertionError("SecurityAutonomyFixture must own the shared flow fixture API")
        base_tree = self._tree(E2E_DIR / "deploy/__init__.py")
        base = self._class(base_tree, "BaseDeployer")
        abstract_methods = {
            node.name
            for node in base.body
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
            and any(self._name(decorator).endswith("abstractmethod") for decorator in node.decorator_list)
        }
        required_methods = {"deploy", "verify"}
        if missing := required_methods - abstract_methods:
            raise AssertionError(f"BaseDeployer misses abstract lifecycle methods: {sorted(missing)}")
        for relative, class_name in (
            ("deploy/base/kubernetes.py", "KubernetesDeployer"),
            ("deploy/base/docker.py", "DockerDeployer"),
            ("deploy/authguard/__init__.py", "AuthGuardDeployer"),
            ("deploy/flowgent/__init__.py", "FlowgentDeployer"),
            ("deploy/sonarqube/__init__.py", "SonarQubeDeployer"),
        ):
            candidate = self._class(self._tree(E2E_DIR / relative), class_name)
            if "BaseDeployer" not in {self._name(base) for base in candidate.bases}:
                raise AssertionError(f"{class_name} must inherit BaseDeployer")
        for relative, class_name in (
            ("deploy/base/kubernetes.py", "KubernetesDeployer"),
            ("deploy/base/docker.py", "DockerDeployer"),
        ):
            candidate = self._class(self._tree(E2E_DIR / relative), class_name)
            methods = {
                node.name for node in candidate.body if isinstance(node, ast.FunctionDef)
            }
            required_backend_methods = {
                "deploy_sonarqube",
                "reset_database",
                "build_images",
                "verify_authguard",
                "import_config",
                "runtime_exec",
            }
            if missing := required_backend_methods - methods:
                raise AssertionError(f"{class_name} misses topology lifecycle methods: {sorted(missing)}")

    def _verify_verifiers(self) -> None:
        procedural_modules = []
        for path in sorted(E2E_DIR.rglob("*.py")):
            # The browser suite has an isolated virtual environment below the
            # E2E root. Its third-party implementation files are not verifier
            # modules and must not participate in the source-structure audit.
            if ".venv" in path.relative_to(E2E_DIR).parts:
                continue
            tree = self._tree(path)
            functions = [
                node.name
                for node in tree.body
                if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
            ]
            if functions:
                procedural_modules.append(
                    f"{path.relative_to(E2E_DIR)}: {', '.join(functions)}"
                )
        if procedural_modules:
            raise AssertionError(
                "all E2E module behavior must be class-owned; procedural functions found: "
                + "; ".join(procedural_modules)
            )
        for scenario_id, (_, module_name) in SCENARIOS.items():
            path = E2E_DIR / (module_name.replace(".", "/") + ".py")
            tree = self._tree(path)
            classes = [node for node in tree.body if isinstance(node, ast.ClassDef)]
            verifier_classes = [
                node for node in classes
                if node.name.endswith("Verifier")
                and any(self._name(base) == "BaseVerifier" for base in node.bases)
            ]
            if len(verifier_classes) != 1:
                raise AssertionError(
                    f"scenario {scenario_id} must declare exactly one XxxVerifier(BaseVerifier): {path}"
                )
            if len(classes) != 1:
                raise AssertionError(
                    f"scenario {scenario_id} must not retain helper or wrapper classes: {path}"
                )
            legacy_classes = [
                node.name
                for node in classes
                if node.name.endswith(("Checks", "Operations"))
            ]
            if legacy_classes:
                raise AssertionError(f"scenario {scenario_id} retains legacy class names: {legacy_classes}")
            methods = {
                method.name
                for method in verifier_classes[0].body
                if isinstance(method, ast.FunctionDef)
            }
            if "run" not in methods or not any(method.startswith("_verify") or method == "_run_scenario" for method in methods):
                raise AssertionError(
                    f"scenario {scenario_id} must keep its execution boundary inside its Verifier class: {path}"
                )
            legacy_entry = any(
                isinstance(node, ast.Assign)
                and any(
                    isinstance(target, ast.Name) and target.id == "VERIFIER_CLASS"
                    for target in node.targets
                )
                for node in tree.body
            )
            if legacy_entry:
                raise AssertionError(f"scenario {scenario_id} retains legacy VERIFIER_CLASS entry: {path}")

    def _verify_parity(self) -> None:
        compose_path = E2E_DIR / "deploy/docker/compose.yml"
        compose = yaml.safe_load(compose_path.read_text(encoding="utf-8"))
        if compose.get("x-e2e-project-name") != "e2e-flowgent-docker":
            raise AssertionError("Docker Compose project must use isolated e2e-flowgent- prefix")
        services = set(compose.get("services", {}))
        required = {
            "flowgent",
            "web",
            "postgres",
            "emqx",
            "jaeger",
            "authguard-authn",
            "authguard-authz",
            "authguard-web",
            "authguard-redis-0",
            "authguard-redis-1",
            "authguard-redis-2",
            "ldap",
            "mock-github",
            "notification-receiver",
            "envoy",
        }
        if missing := required - services:
            raise AssertionError(f"Docker topology misses functional-equivalence services: {sorted(missing)}")
        runner = (E2E_DIR / "runner.py").read_text(encoding="utf-8")
        if 'choices=("k8s", "docker")' not in runner:
            raise AssertionError("runner does not expose the two deployer modes")
        if "DEFAULT_SCENARIOS" not in runner:
            raise AssertionError("both deployers must consume the same default verifier matrix")

    def _verify_image_importer(self) -> None:
        tree = self._tree(E2E_DIR / "deploy/base/kubernetes.py")
        classes = {
            node.name for node in tree.body if isinstance(node, ast.ClassDef)
        }
        required_classes = {
            "KubernetesLocalImageLoader",
            "KubernetesImageManager",
            "KubernetesManualAccess",
        }
        if missing := required_classes - classes:
            raise AssertionError(f"K8s image importer misses classes: {sorted(missing)}")
        manager = self._class(tree, "KubernetesImageManager")
        manager_methods = {
            node.name for node in manager.body if isinstance(node, ast.FunctionDef)
        }
        required_methods = {"import_core", "import_web", "import_authguard", "import_authguard_web"}
        if missing := required_methods - manager_methods:
            raise AssertionError(f"K8s image manager misses methods: {sorted(missing)}")

    def _verify_gateway_browser(self) -> None:
        browser = (E2E_DIR / "verifier/web/browser.py").read_text(encoding="utf-8")
        hosted_login = (E2E_DIR / "verifier/web/browser.py").read_text(
            encoding="utf-8"
        )
        envoy = (E2E_DIR / "config/envoy/envoy.yaml").read_text(encoding="utf-8")
        chart = (E2E_DIR.parents[2] / "deploy/helm/flowgent/templates/web.yaml").read_text(
            encoding="utf-8"
        )
        values = (E2E_DIR.parents[2] / "deploy/helm/flowgent/values.yaml").read_text(
            encoding="utf-8"
        )
        if "LOCAL_GATEWAY_PORT" not in browser or "/auth/login?return_to=" not in browser:
            raise AssertionError("browser scenarios must enter Hosted Login through Envoy Gateway")
        if any(port in browser for port in ("21080", "31080")):
            raise AssertionError("browser scenarios must not bypass Gateway through a Web NodePort")
        for proof in (
            'fetch(\'/auth/session\')',
            'response.url.endswith("/auth/logout")',
            "session_before != 200",
            "session_after != 401",
        ):
            if proof not in hosted_login:
                raise AssertionError(f"real browser sign-out proof is missing: {proof}")
        if "cluster: flowgent_web" not in envoy or 'prefix: "/api/"' not in envoy:
            raise AssertionError("Docker edge must separate the public SPA from protected /api/")
        if "kind: HTTPRoute" not in chart or "value: /" not in chart:
            raise AssertionError("Flowgent Helm must expose its public SPA through the shared Gateway")
        if "path: /api/" not in values:
            raise AssertionError("AuthGuard's protected Flowgent route must be scoped to /api/")

    def _verify_forbidden_paths(self) -> None:
        forbidden = (
            "common/access.py",
            "common/api.py",
            "common/base",
            "common/base_deployer.py",
            "common/db.py",
            "common/docker_deployer.py",
            "common/images.py",
            "common/kubernetes_deployer.py",
            "common/state.py",
            "common/suite.py",
            "deploy/component.py",
            "deploy/authguard.py",
            "deploy/flowgent.py",
            "deploy/sonarqube.py",
            "deploy/docker-compose.yml",
            "support",
            "verifier/agentflow/base.py",
            "verifier/core/base.py",
            "verifier/infra/base.py",
        )
        existing = [relative for relative in forbidden if (E2E_DIR / relative).exists()]
        if existing:
            raise AssertionError(f"obsolete E2E modules still exist: {existing}")

    @staticmethod
    def _tree(path: Path) -> ast.Module:
        return ast.parse(path.read_text(encoding="utf-8"), filename=str(path))

    @classmethod
    def _class(cls, tree: ast.Module, name: str) -> ast.ClassDef:
        for node in tree.body:
            if isinstance(node, ast.ClassDef) and node.name == name:
                return node
        raise AssertionError(f"missing class {name}")

    @classmethod
    def _name(cls, node: ast.expr) -> str:
        if isinstance(node, ast.Name):
            return node.id
        if isinstance(node, ast.Attribute):
            return f"{cls._name(node.value)}.{node.attr}"
        return ""
