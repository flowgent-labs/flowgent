"""Validate application resources and data scopes before any deployment or mutation."""

from __future__ import annotations

import re

from util.constant import E2EOtherDefinitions as config
from verifier.context import BaseVerifier, VerificationContext


class E2ES01PreflightVerifier(BaseVerifier):
    scenario_id = "01"
    title = "AutoTest — Application Resource and Data-Scope Preflight"

    def deploy(self, context: VerificationContext) -> None:
        """Preflight never deploys or mutates a provider."""

    def init(self, context: VerificationContext) -> None:
        import yaml

        self.resources = [
            yaml.safe_load(path.read_text())
            for path in sorted(config.CONFIG_DIR.rglob("*.yaml"))
        ]

    def verifier(self, context: VerificationContext) -> None:
        problems = self.problems(self.resources)
        self.details.extend(problems)
        if problems:
            raise AssertionError(
                "Application preflight failed:\n- " + "\n- ".join(problems)
            )
        self.details.append("All referenced resources and item scopes are defined")

    @staticmethod
    def problems(resources: list[dict]) -> list[str]:
        resources = [doc for doc in resources if isinstance(doc, dict)]
        available = {
            (
                doc.get("kind", "").lower(),
                doc.get("metadata", {}).get(
                    "name", doc.get("name", doc.get("data", {}).get("name"))
                ),
            )
            for doc in resources
        }
        problems = []
        flows = [doc.get("data", doc) for doc in resources if doc.get("kind") == "Flow"]
        if not flows:
            return ["No Flow manifest is defined"]

        def check(node, in_map=False):
            identifier = node.get("id", "<missing>")
            for field, kind in (("agent", "agent"), ("tool", "mcp")):
                reference = node.get(field)
                if isinstance(reference, str) and (kind, reference) not in available:
                    problems.append(
                        f"{identifier}: missing {kind} resource {reference}"
                    )
            if (
                not in_map
                and re.search(r"\$\{item(?:[.}]|\[)", str(node))
                and node.get("kind") != "map"
            ):
                problems.append(
                    f"{identifier}: item reference escapes its map child scope"
                )
            if node.get("kind") in {"map", "agentflow", "skill"}:
                problems.append(
                    f"{identifier}: {node['kind']} execution is not implemented by the documented runtime"
                )
            child = (
                node.get("node")
                or node.get("child")
                or node.get("map", {}).get("child")
                if isinstance(node.get("map", {}), dict)
                else None
            )
            if isinstance(child, dict):
                check(child, True)

        for flow in flows:
            nodes = flow.get("nodes", [])
            names = [node.get("id") for node in nodes]
            if not nodes or None in names or len(names) != len(set(names)):
                problems.append("Flow has missing or duplicate node IDs")
            incoming = {name: set() for name in names}
            for edge in flow.get("edges", []):
                if edge.get("from") not in incoming or edge.get("to") not in incoming:
                    problems.append("Flow has an edge referencing a missing node")
                else:
                    incoming[edge["to"]].add(edge["from"])
            processed = set()
            while True:
                ready = {
                    name
                    for name, parents in incoming.items()
                    if name not in processed and parents <= processed
                }
                if not ready:
                    break
                processed.update(ready)
            if len(processed) != len(incoming):
                problems.append(
                    "Flow has a feedback cycle; repeated-node execution is not implemented"
                )
            for node in nodes:
                check(node)
        return sorted(set(problems))
