"""Guard the AuthGuard-aligned E2E layering and dual-deployer parity."""

from __future__ import annotations

import ast
import os
from pathlib import Path

import yaml
from util.constant import E2EOtherDefinitions as config

E2E_DIR = config.E2E_DIR
SCENARIOS = config.SCENARIOS
from util.report import VerificationResult

from verifier.context import BaseVerifier, VerificationContext


class E2ES01StructureVerifier(BaseVerifier):
    scenario_id = "01"
    title = "E2E Structure — Layered Deployment and Verifier Contracts"

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(self._run_scenario)

    def _run_scenario(self) -> None:
        self.step(
            "verify shared deployer abstraction and both backends",
            self._verify_deployers,
        )
        self.step("verify direct BaseVerifier scenario classes", self._verify_verifiers)
        self.step("verify Docker and K8s functional matrix parity", self._verify_parity)
        self.step(
            "verify same-origin Hosted Login and sign-out routing",
            self._verify_gateway_browser,
        )
        self.step(
            "verify K8s image-runtime discovery contract", self._verify_image_importer
        )
        self.step(
            "verify namespace isolation and production-style resource names",
            self._verify_namespace_contract,
        )
        self.step(
            "verify obsolete procedural modules are absent",
            self._verify_forbidden_paths,
        )

    def _verify_deployers(self) -> None:
        required = (
            "runner.py",
            "verifier/context.py",
            "util/process.py",
            "util/images.py",
            "util/kubernetes.py",
            "util/docker.py",
            "util/report.py",
            "util/constant.py",
            "verifier/infra/_kubernetes.py",
            "verifier/infra/_docker.py",
            "verifier/infra/_gateway_post_renderer.py",
            "verifier/agentgraph/__init__.py",
        )
        for relative in required:
            if not (E2E_DIR / relative).is_file():
                raise AssertionError(f"missing E2E layer: {relative}")
        allowed = {
            "__init__.py",
            "process.py",
            "images.py",
            "postgres.py",
            "pgvector.py",
            "sqlite.py",
            "kubernetes.py",
            "docker.py",
            "report.py",
            "constant.py",
        }
        unexpected = {path.name for path in (E2E_DIR / "util").glob("*.py")} - allowed
        if unexpected:
            raise AssertionError(f"non-generic utility files: {sorted(unexpected)}")
        project = E2E_DIR / "deploy/flowgent"
        for path in project.iterdir():
            if path.is_symlink():
                if not path.exists() or not Path(
                    os.path.abspath(path.parent / path.readlink())
                ).is_relative_to(config.PROJECT_ROOT / "deploy"):
                    raise AssertionError(f"Invalid official deployment link: {path}")
            elif path.name not in {"config", "docker-compose.yaml"}:
                raise AssertionError(
                    f"Non-configuration content in self-project deployment: {path.name}"
                )
        for name in ("Dockerfile", "Dockerfile.dockerignore"):
            if not (project / name).is_symlink():
                raise AssertionError(
                    f"Self-project immutable asset must be a link: {name}"
                )
        if (project / "helm").exists() or (project / "helm").is_symlink():
            raise AssertionError("Use cases must load the formal Helm chart directly")
        if config.HELM_CHART != config.PROJECT_ROOT / "deploy/helm/flowgent":
            raise AssertionError("Helm entry point must be the formal root chart")
        for path in (E2E_DIR / "deploy").iterdir():
            if not path.is_dir() or path.name == "__pycache__":
                continue
            if not (path / "docker-compose.yaml").is_file():
                raise AssertionError(
                    f"component has no Compose definition: {path.name}"
                )
            chart = config.HELM_CHART if path.name == "flowgent" else path / "helm"
            if not (chart / "Chart.yaml").is_file():
                raise AssertionError(f"component has no Helm definition: {path.name}")
            if path.name.startswith("mock-"):
                if (
                    not (path / "source").is_dir()
                    or not (path / "Dockerfile").is_file()
                ):
                    raise AssertionError(
                        f"mock component has no independent source/build: {path.name}"
                    )
        base = self._class(self._tree(E2E_DIR / "verifier/context.py"), "BaseDeployer")
        methods = {n.name for n in base.body if isinstance(n, ast.FunctionDef)}
        if not {"deploy", "verify", "prepare"} <= methods:
            raise AssertionError("shared deployment lifecycle is incomplete")

    def _verify_verifiers(self) -> None:
        for scenario_id, (_, module_name) in SCENARIOS.items():
            path = E2E_DIR / (module_name.replace(".", "/") + ".py")
            tree = self._tree(path)
            if any(
                isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
                for node in tree.body
            ):
                raise AssertionError(f"scenario has bare functions: {path}")
            classes = [node for node in tree.body if isinstance(node, ast.ClassDef)]
            expected = (
                "E2ES"
                + scenario_id
                + "".join(part.title() for part in path.stem[4:].split("_"))
                + "Verifier"
            )
            if len(classes) != 1 or classes[0].name != expected:
                raise AssertionError(f"expected exactly one {expected} in {path}")
            methods = {
                n.name for n in classes[0].body if isinstance(n, ast.FunctionDef)
            }
            if not {"deploy", "init", "verifier"} <= methods:
                raise AssertionError(f"scenario lifecycle is incomplete: {path}")
            if "VerificationContext" not in path.read_text():
                raise AssertionError(f"scenario has no shared context: {path}")

    def _verify_parity(self) -> None:
        services = set()
        for compose_path in (E2E_DIR / "deploy").glob("*/docker-compose.yaml"):
            # Inspect service names without interpreting Compose-specific tags.
            compose = yaml.load(compose_path.read_text(encoding="utf-8"), Loader=yaml.BaseLoader)
            services.update(compose.get("services", {}))
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
            "mock-llm",
            "envoy",
        }
        if missing := required - services:
            raise AssertionError(
                f"Docker topology misses functional-equivalence services: {sorted(missing)}"
            )
        runner = (E2E_DIR / "runner.py").read_text(encoding="utf-8")
        if 'choices=("k8s", "docker")' not in runner:
            raise AssertionError("runner does not expose the two deployer modes")
        if "DEFAULT_SCENARIOS" not in runner:
            raise AssertionError(
                "both deployers must consume the same default verifier matrix"
            )

    def _verify_image_importer(self) -> None:
        from util.docker import E2EDockerDeployer
        from util.images import E2EImagesDefinitions
        from util.kubernetes import E2EKubernetesDeployer, KubernetesLocalImageLoader

        for kind in ("colima", "orbstack", "docker", "k3s", "containerd"):
            if kind not in KubernetesLocalImageLoader._SUPPORTED:
                raise AssertionError(f"local image runtime is unsupported: {kind}")
        if not callable(E2EKubernetesDeployer.resource) or not callable(
            E2EDockerDeployer.detect
        ):
            raise AssertionError("shared resource/runtime boundary is incomplete")
        if any(len(pair) != 2 for pair in E2EImagesDefinitions.MATRIX.values()):
            raise AssertionError("images must declare upstream and mirror provenance")

    def _verify_namespace_contract(self) -> None:
        expected_namespaces = {
            "e2e-flowgent-system",
            "e2e-flowgent-authguard-system",
            "e2e-flowgent-mock-github",
            "e2e-flowgent-mock-notification",
            "e2e-flowgent-mock-llm",
            "e2e-flowgent-storage-system",
            "e2e-flowgent-sonarqube-system",
            "e2e-flowgent-workload-security-fixer",
        }
        if set(config.K8S_MANAGED_NAMESPACES) != expected_namespaces:
            raise AssertionError(
                "K8s namespace topology drifted: "
                f"{sorted(config.K8S_MANAGED_NAMESPACES)}"
            )
        if config.RELEASE_NAME != "flowgent":
            raise AssertionError(
                "business release must use the production-style name flowgent"
            )
        source = "\n".join(
            (E2E_DIR / relative).read_text(encoding="utf-8")
            for relative in (
                "verifier/infra/_authguard.py",
                "verifier/infra/_application.py",
                "verifier/infra/_postgres.py",
                "verifier/infra/_mock_llm.py",
                "verifier/infra/_sonarqube.py",
            )
        )
        for obsolete in (
            "mocksvc-github-service",
            "mocksvc-notification-service",
            'PGVECTOR_NAME = f"{config.RESOURCE_PREFIX}',
            'SONARQUBE_NAME = f"{config.RESOURCE_PREFIX}',
        ):
            if obsolete in source:
                raise AssertionError(
                    f"obsolete E2E-prefixed resource contract remains: {obsolete}"
                )
        chart_root = config.PROJECT_ROOT / "deploy/helm/flowgent"
        apiserver = (chart_root / "templates/apiserver.yaml").read_text(
            encoding="utf-8"
        )
        web = (chart_root / "templates/web.yaml").read_text(encoding="utf-8")
        for rendered in (apiserver, web):
            if "gatewayNamespace" not in rendered:
                raise AssertionError(
                    "cross-namespace Gateway parentRef is not explicit"
                )
        if 'authguard.io/protected: "true"' not in apiserver:
            raise AssertionError(
                "Flowgent API route is not selected by AuthGuard SecurityPolicy"
            )
        if (
            "kind: ReferenceGrant" not in apiserver
            or "kind: SecurityPolicy" not in apiserver
        ):
            raise AssertionError(
                "cross-namespace AuthGuard SecurityPolicy ReferenceGrant is missing"
            )

    def _verify_gateway_browser(self) -> None:
        browser = (E2E_DIR / "verifier/web/__init__.py").read_text(encoding="utf-8")
        hosted_login = (E2E_DIR / "verifier/web/__init__.py").read_text(
            encoding="utf-8"
        )
        web_main = (config.PROJECT_ROOT / "web/src/main.tsx").read_text(
            encoding="utf-8"
        )
        auth_boundary = (
            config.PROJECT_ROOT / "web/src/app/authentication-boundary.tsx"
        ).read_text(encoding="utf-8")
        runtime_config = (
            config.PROJECT_ROOT / "web/src/app/runtime-config.ts"
        ).read_text(encoding="utf-8")
        envoy = (config.CONFIG_DIR / "envoy/envoy.yaml").read_text(encoding="utf-8")
        chart = (
            config.PROJECT_ROOT / "deploy/helm/flowgent/templates/web.yaml"
        ).read_text(encoding="utf-8")
        values = (config.PROJECT_ROOT / "deploy/helm/flowgent/values.yaml").read_text(
            encoding="utf-8"
        )
        deployer = (E2E_DIR / "verifier/infra/_application.py").read_text(
            encoding="utf-8"
        )
        if (
            "LOCAL_GATEWAY_PORT" not in browser
            or "/auth/login?return_to=" not in browser
        ):
            raise AssertionError(
                "browser scenarios must enter Hosted Login through Envoy Gateway"
            )
        if any(port in browser for port in ("21080", "31080")):
            raise AssertionError(
                "browser scenarios must not bypass Gateway through a Web NodePort"
            )
        if "AuthenticationBoundary" not in web_main:
            raise AssertionError(
                "Flowgent Web must gate the console on an AuthGuard session"
            )
        for proof in ("/auth/session", "response.status === 401", "hostedLoginUrl"):
            if proof not in auth_boundary:
                raise AssertionError(
                    f"Flowgent Web authentication boundary is missing: {proof}"
                )
        for proof in ("/runtime-config.json", "setRuntimeNamespace"):
            if proof not in runtime_config:
                raise AssertionError(
                    f"Flowgent Web runtime namespace config is missing: {proof}"
                )
        for proof in (
            "fetch('/auth/session')",
            'response.url.endswith("/auth/logout")',
            "session_before != 200",
            "session_after != 401",
        ):
            if proof not in hosted_login:
                raise AssertionError(f"real browser sign-out proof is missing: {proof}")
        if "cluster: flowgent_web" not in envoy or 'prefix: "/api/"' not in envoy:
            raise AssertionError(
                "Docker edge must separate the public SPA from protected /api/"
            )
        if "kind: HTTPRoute" not in chart or "value: /" not in chart:
            raise AssertionError(
                "Flowgent Helm must expose its public SPA through the shared Gateway"
            )
        if "FLOWGENT_DEFAULT_NAMESPACE" not in chart:
            raise AssertionError(
                "Flowgent Helm must pass the runtime namespace to the Web console"
            )
        controller_chart = (
            config.PROJECT_ROOT / "deploy/helm/flowgent/templates/controller.yaml"
        ).read_text(encoding="utf-8")
        if "flowgent.runtimeCredentialEnvFrom" not in controller_chart:
            raise AssertionError(
                "Flowgent Controller must receive the configured runtime credential Secret"
            )
        helpers = (
            config.PROJECT_ROOT / "deploy/helm/flowgent/templates/_helpers.tpl"
        ).read_text(encoding="utf-8")
        if "-emqx.%s.svc.cluster.local:1883" not in helpers:
            raise AssertionError(
                "Flowgent Helm must use a cross-namespace MQTT service FQDN"
            )
        if "path: /api/" not in values:
            raise AssertionError(
                "AuthGuard's protected Flowgent route must be scoped to /api/"
            )
        if "web.apiUpstream=http://{release}-apiserver:9999" not in deployer:
            raise AssertionError(
                "Web must proxy API calls to the protected external listener"
            )
        if "web.apiUpstream=http://{release}-apiserver:9990" in deployer:
            raise AssertionError(
                "Web must never proxy to Flowgent's trusted internal listener"
            )
        if "apiserver.service.type=NodePort" in deployer:
            raise AssertionError(
                "E2E must not expose Flowgent's trusted internal API as a NodePort"
            )
        compose = (E2E_DIR / "deploy/flowgent/docker-compose.yaml").read_text(
            encoding="utf-8"
        )
        if "FLOWGENT_API_UPSTREAM: http://flowgent:9999" not in compose:
            raise AssertionError(
                "Docker Web must proxy API calls to the protected external listener"
            )
        if "FLOWGENT_API_UPSTREAM: http://flowgent:9990" in compose:
            raise AssertionError(
                "Docker Web must never proxy to Flowgent's trusted internal listener"
            )

    def _verify_forbidden_paths(self) -> None:
        forbidden = (
            "e2e",
            "common",
            "deploy/base",
            "deploy/docker",
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
            "deploy/mocksvc-github-service",
            "deploy/mocksvc-notification-service",
            "support",
            "verifier/agentgraph/base.py",
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

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        """Retain initialization ordering inside the existing scenario steps."""
