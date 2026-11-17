"""Read real deployment, API and database readiness without creating business data."""

from contextlib import closing

from util.constant import E2EOtherDefinitions as config
from util.kubernetes import E2EKubernetesDeployer
from util.postgres import E2EPostgresChecker
from verifier.context import BaseVerifier, VerificationContext


class E2ES11InfrastructureVerifier(BaseVerifier):
    scenario_id = "11"
    title = "AutoTest — Infrastructure Readiness"

    def deploy(self, context: VerificationContext) -> None:
        """The runner owns the requested infrastructure deployment."""

    def init(self, context: VerificationContext) -> None:
        """Readiness checks do not create synthetic application records."""

    def verifier(self, context: VerificationContext) -> None:
        context.request("/_/healthz")
        pods = E2EKubernetesDeployer.resource(
            "pods",
            context.namespace,
            selector=f"app.kubernetes.io/instance={context.release}",
        ).get("items", [])
        if not pods or not all(
            pod.get("status", {}).get("containerStatuses")
            and all(
                container.get("ready")
                for container in pod["status"]["containerStatuses"]
            )
            for pod in pods
        ):
            raise AssertionError(
                "The selected AutoTest deployment is absent or not ready"
            )
        with closing(E2EPostgresChecker.connect(config.postgres_dsn())) as connection:
            if E2EPostgresChecker.query(connection, "SELECT 1") != [(1,)]:
                raise AssertionError(
                    "The configured PostgreSQL connection is not usable"
                )
        self.details.append(
            "Selected deployment, canonical API and configured PG are ready"
        )
