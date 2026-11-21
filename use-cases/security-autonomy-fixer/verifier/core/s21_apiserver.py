#!/usr/bin/env python3
"""Scenario 21: canonical Flowgent REST CRUD and PostgreSQL persistence.

This verifier intentionally speaks only the post-refactor contract. It proves
stable identities, immutable revisions, canonical Run/NodeRun fields, unified
approvals, reviewed Knowledge publication, and the non-versioned MCP/LLM/
notification resources against the same API used by the Web console.
"""

from __future__ import annotations

import time
import uuid
from typing import Any

import requests
from util.constant import E2EOtherDefinitions as config
from util.report import VerificationResult

from verifier import context as common_api
from verifier.context import BaseVerifier, FlowgentE2EProject, VerificationContext
from verifier.core import ApiLifecycleMqttClient

try:
    import psycopg2  # noqa: F401

    PG_AVAILABLE = True
except ImportError:
    PG_AVAILABLE = False


API_BASE = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID


class E2ES21ApiserverVerifier(BaseVerifier):
    scenario_id = "21"
    title = "API Server — Canonical CRUD, Revisions, Run State, and Publication"

    @staticmethod
    def suffix() -> str:
        return uuid.uuid4().hex[:8]

    @staticmethod
    def request(
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        expected: tuple[int, ...] = (200,),
        timeout: int = 20,
    ) -> Any:
        response = requests.request(
            method,
            f"{API_BASE}{path}",
            json=payload,
            headers=common_api.FlowgentE2EProject.headers(),
            timeout=timeout,
        )
        if response.status_code not in expected:
            raise AssertionError(
                f"{method} {path}: HTTP {response.status_code}, expected {expected}: "
                f"{response.text[:800]}"
            )
        if response.status_code == 204 or not response.text:
            return {}
        return response.json()

    @staticmethod
    def pg_connect():
        if not PG_AVAILABLE:
            raise AssertionError(
                "psycopg2 is required for canonical persistence evidence"
            )
        connection = FlowgentE2EProject.pg_connect()
        if connection is None:
            raise AssertionError("PostgreSQL connection is unavailable")
        return connection

    @staticmethod
    def scalar(connection, sql: str, args: tuple[Any, ...] = ()) -> Any:
        with connection.cursor() as cursor:
            cursor.execute(sql, args)
            row = cursor.fetchone()
        if row is None:
            raise AssertionError(f"query returned no row: {sql}")
        return row[0]

    @staticmethod
    def graph_body(
        name: str, description: str, summarize: bool = True
    ) -> dict[str, Any]:
        return {
            "name": name,
            "kind": "graph",
            "description": description,
            "summarize_enabled": summarize,
            "nodes": [{"id": "noop", "kind": "noop"}],
            "edges": [],
            "runtime_mode": "session",
        }

    def verifier(self, context: VerificationContext) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "verify canonical REST resources and PostgreSQL persistence",
                self._verify,
            )
        )

    def _verify(self) -> None:
        connection = self.pg_connect()
        suffix = self.suffix()
        agent_graph_name = f"api-graph-{suffix}"
        agent_name = f"api-agent-{suffix}"
        skill_name = f"api-skill-{suffix}"
        mcp_name = f"api-mcp-{suffix}"
        llm_name = f"api-llm-{suffix}"
        channel_name = f"api-channel-{suffix}"
        graph_created = False
        graph_run_id = ""
        cleanup: list[tuple[str, str]] = []

        try:
            graph = self._graph_crud(connection, agent_graph_name)
            graph_created = True
            self._graph_lifecycle_event()
            self._agent_crud(connection, agent_name)
            self._skill_crud(connection, skill_name)
            self._mcp_crud(connection, mcp_name)
            self._llm_crud(connection, llm_name)
            self._notification_crud(connection, channel_name)
            graph_run_id = self._run_node_approval_and_publication(
                connection, agent_graph_name, graph
            )
            self.details.extend(
                [
                    f"AgentGraph {agent_graph_name} kept stable id {graph['id']} while revision advanced to 2.",
                    "Agent and Skill saves produced immutable revision 2 rows.",
                    "MCP, LLM, and notification definitions completed canonical CRUD without version aliases.",
                    f"Run {graph_run_id} persisted a locked AgentGraph revision, NodeRun attempt/fencing state, and one unified approval.",
                    "Knowledge candidate stayed invisible until approval, then published one immutable document revision.",
                ]
            )
        finally:
            if graph_run_id:
                self._ignore("DELETE", f"/api/v1/{NAMESPACE}/graph-runs/{graph_run_id}")
            if graph_created:
                self._ignore("DELETE", f"/api/v1/{NAMESPACE}/agent-graphs/{agent_graph_name}")
            for method, path in reversed(cleanup):
                self._ignore(method, path)
            connection.close()

    def _graph_crud(self, connection, name: str) -> dict[str, Any]:
        base = f"/api/v1/{NAMESPACE}/agent-graphs"
        created = self.request(
            "POST", base, self.graph_body(name, "canonical API lifecycle"), (201,)
        )
        if created.get("name") != name or not created.get("id"):
            raise AssertionError(f"AgentGraph create omitted stable id/name: {created}")
        if created.get("revision") != 1:
            raise AssertionError(f"AgentGraph create revision is not 1: {created}")
        stable_id = created["id"]
        persisted = self.scalar(
            connection,
            """SELECT COUNT(*) FROM orh_agent_graph f
               JOIN orh_agent_graph_revision r ON r.id=f.current_revision_id
               WHERE f.id=%s AND f.namespace_id=%s AND f.name=%s
                 AND f.status<>'DELETED' AND r.agent_graph_id=f.id AND r.revision=1""",
            (stable_id, NAMESPACE, name),
        )
        if persisted != 1:
            raise AssertionError(
                "AgentGraph stable identity/current revision was not persisted"
            )

        fetched = self.request("GET", f"{base}/{name}")
        if fetched.get("id") != stable_id or fetched.get("name") != name:
            raise AssertionError(f"AgentGraph GET changed identity: {fetched}")
        listed = self.request("GET", base)
        if not any(
            item.get("id") == stable_id and item.get("name") == name for item in listed
        ):
            raise AssertionError("AgentGraph is absent from canonical LIST")

        updated_body = self.graph_body(name, "canonical revision two")
        updated_body["id"] = stable_id
        updated = self.request("PUT", f"{base}/{name}", updated_body)
        if updated.get("id") != stable_id or updated.get("revision") != 2:
            raise AssertionError(
                f"AgentGraph update did not preserve id/create revision 2: {updated}"
            )
        revision_state = self.scalar(
            connection,
            """SELECT COUNT(*)=2 AND MAX(r.revision)=2
               FROM orh_agent_graph_revision r WHERE r.agent_graph_id=%s""",
            (stable_id,),
        )
        if not revision_state:
            raise AssertionError("AgentGraph immutable revision history is incomplete")
        return updated

    def _graph_lifecycle_event(self) -> None:
        mqtt = ApiLifecycleMqttClient(config.EMQX_HOST, config.EMQX_PORT)
        if not mqtt.client:
            raise AssertionError("MQTT is unavailable for lifecycle event verification")
        temporary = f"event-graph-{self.suffix()}"
        try:
            mqtt.subscribe("flowgent/v1/+/agent-graphs/+/ctrl/graph/updated")
            mqtt.subscribe("flowgent/v1/+/agent-graphs/+/ctrl/graph/deleted")
            time.sleep(0.5)
            created = self.request(
                "POST",
                f"/api/v1/{NAMESPACE}/agent-graphs",
                self.graph_body(temporary, "event evidence", False),
                (201,),
            )
            event = mqtt.wait_for_message("ctrl/graph/updated", timeout=8)
            if not event:
                raise AssertionError("AgentGraph create lifecycle event was not received")
            created_payload = event.get("payload", {})
            if (
                created_payload.get("agent_graph_id") != created["id"]
                or created_payload.get("agent_graph_name") != temporary
                or created_payload.get("event_type") != "CREATED"
            ):
                raise AssertionError(
                    f"AgentGraph create lifecycle payload is not canonical: {event}"
                )
            self.request(
                "DELETE", f"/api/v1/{NAMESPACE}/agent-graphs/{temporary}", expected=(204,)
            )
            deleted = mqtt.wait_for_message("ctrl/graph/deleted", timeout=8)
            if not deleted:
                raise AssertionError("AgentGraph delete lifecycle event was not received")
            deleted_payload = deleted.get("payload", {})
            if (
                deleted_payload.get("agent_graph_id") != created["id"]
                or deleted_payload.get("agent_graph_name") != temporary
                or deleted_payload.get("event_type") != "DELETED"
            ):
                raise AssertionError(
                    f"AgentGraph delete lifecycle payload is not canonical: {deleted}"
                )
        finally:
            self._ignore("DELETE", f"/api/v1/{NAMESPACE}/agent-graphs/{temporary}")
            mqtt.close()

    def _agent_crud(self, connection, name: str) -> None:
        base = f"/api/v1/{NAMESPACE}/agents"
        body = {
            "name": name,
            "model": "openai/e2e",
            "soul": "Evidence first.",
            "instruction": "Return a structured result.",
            "input_schema": {"type": "object"},
            "output_schema": {"type": "object"},
        }
        created = self.request("POST", base, body, (201,))
        stable_id = created.get("id")
        if not stable_id or created.get("revision") != 1:
            raise AssertionError(f"Agent identity/revision invalid: {created}")
        body["instruction"] = "Return a structured result with evidence."
        updated = self.request("PUT", f"{base}/{name}", body)
        if updated.get("id") != stable_id or updated.get("revision") != 2:
            raise AssertionError(f"Agent revision 2 invalid: {updated}")
        if (
            self.scalar(
                connection,
                "SELECT COUNT(*) FROM llm_agent_revision WHERE agent_id=%s",
                (stable_id,),
            )
            != 2
        ):
            raise AssertionError("Agent revision history was not persisted")
        self.request("DELETE", f"{base}/{name}", expected=(204,))
        if (
            self.scalar(
                connection, "SELECT status FROM llm_agent WHERE id=%s", (stable_id,)
            )
            != "DELETED"
        ):
            raise AssertionError("Agent soft deletion did not use status")

    def _skill_crud(self, connection, name: str) -> None:
        base = f"/api/v1/{NAMESPACE}/skill-definitions"
        body = {
            "name": name,
            "description": "API-managed reusable skill",
            "instruction": "Use only reviewed assets.",
            "model": "openai/e2e",
            "tools": [],
        }
        created = self.request("POST", base, body, (201,))
        stable_id = created.get("id")
        if not stable_id or created.get("revision") != 1:
            raise AssertionError(f"Skill identity/revision invalid: {created}")
        body["instruction"] = "Use only reviewed assets and cite them."
        updated = self.request("PUT", f"{base}/{name}", body)
        if updated.get("id") != stable_id or updated.get("revision") != 2:
            raise AssertionError(f"Skill revision 2 invalid: {updated}")
        if (
            self.scalar(
                connection,
                "SELECT COUNT(*) FROM llm_skill_revision WHERE skill_id=%s",
                (stable_id,),
            )
            != 2
        ):
            raise AssertionError("Skill revision history was not persisted")
        self.request("DELETE", f"{base}/{name}", expected=(204,))

    def _mcp_crud(self, connection, name: str) -> None:
        base = f"/api/v1/{NAMESPACE}/mcp"
        body = {
            "name": name,
            "transport": "http",
            "rpc_url": "https://mcp.example.test/v1",
            "enabled": True,
            "header_refs": {"Authorization": "${DEEPSEEK_API_KEY}"},
            "env_refs": {"MCP_TENANT": "${DEEPSEEK_API_KEY}"},
        }
        created = self.request("POST", base, body, (201,))
        stable_id = created.get("id")
        body["rpc_url"] = "https://mcp.example.test/v2"
        body["enabled"] = False
        updated = self.request("PUT", f"{base}/{name}", body)
        if updated.get("id") != stable_id or updated.get("rpc_url") != body["rpc_url"]:
            raise AssertionError(f"MCP update failed: {updated}")
        if (
            self.scalar(
                connection, "SELECT rpc_url FROM llm_mcp WHERE id=%s", (stable_id,)
            )
            != body["rpc_url"]
        ):
            raise AssertionError("MCP canonical rpc_url was not persisted")
        self.request("DELETE", f"{base}/{name}", expected=(204,))

    def _llm_crud(self, connection, name: str) -> None:
        base = f"/api/v1/{NAMESPACE}/llm/providers"
        body = {
            "name": name,
            "type": "openai",
            "base_uri": "https://llm.example.test/v1",
            "default_model": "e2e-model",
            "models": [],
            "enabled": True,
            "api_key_env": "DEEPSEEK_API_KEY",
            "env_refs": {"LLM_TENANT": "${DEEPSEEK_API_KEY}"},
        }
        created = self.request("POST", base, body, (201,))
        stable_id = created.get("id")
        body.update(
            {"type": "anthropic", "base_uri": "https://llm.example.test/anthropic"}
        )
        body.pop("api_key_env", None)
        updated = self.request("PUT", f"{base}/{stable_id}", body)
        if (
            updated.get("type") != "anthropic"
            or updated.get("base_uri") != body["base_uri"]
        ):
            raise AssertionError(f"LLM update failed: {updated}")
        row = self.scalar(
            connection,
            "SELECT type || '|' || base_uri FROM llm_provider WHERE id=%s",
            (stable_id,),
        )
        if row != f"anthropic|{body['base_uri']}":
            raise AssertionError("LLM canonical type/base_uri was not persisted")
        self.request("DELETE", f"{base}/{stable_id}", expected=(204,))

    def _notification_crud(self, connection, name: str) -> None:
        base = f"/api/v1/{NAMESPACE}/notifications/channels"
        body = {
            "name": name,
            "provider": "webhook",
            "config": {"url": "https://notify.example.test/e2e"},
            "enabled": True,
        }
        created = self.request("POST", base, body, (201,))
        stable_id = created.get("id")
        if "url" in created.get("config", {}):
            raise AssertionError("Notification response leaked a write-only URL")
        updated = self.request(
            "PUT",
            f"{base}/{stable_id}",
            {"name": name, "provider": "webhook", "config": {}, "enabled": False},
        )
        if updated.get("enabled") is not False:
            raise AssertionError(f"Notification update failed: {updated}")
        if self.scalar(
            connection, "SELECT enabled FROM nfy_channel WHERE id=%s", (stable_id,)
        ):
            raise AssertionError("Notification enabled=false was not persisted")
        self.request("DELETE", f"{base}/{stable_id}", expected=(204,))

    def _run_node_approval_and_publication(
        self, connection, agent_graph_name: str, graph: dict[str, Any]
    ) -> str:
        runs = f"/api/v1/{NAMESPACE}/graph-runs"
        run = self.request(
            "POST",
            runs,
            {
                "agent_graph_name": agent_graph_name,
                "status": "PENDING",
                "input": {"origin": "scenario-21"},
                "runtime_mode": "session",
            },
            (201,),
        )
        graph_run_id = run.get("id")
        if (
            not graph_run_id
            or run.get("agent_graph_id") != graph.get("id")
            or run.get("agent_graph_revision") != 2
        ):
            raise AssertionError(f"Run did not lock canonical AgentGraph revision: {run}")
        if not run.get("context_snapshot") or run.get("summarize_enabled") is not True:
            raise AssertionError(f"Run snapshot/effective summary flag missing: {run}")
        self.request("PUT", f"{runs}/{graph_run_id}", {"status": "RUNNING"})

        tasks = f"{runs}/{graph_run_id}/node-runs"
        task_body = {
            "node_instance_id": "noop",
            "attempt": 1,
            "status": "RUNNING",
            "input": {"step": 1},
            "execution_id": f"exec-{self.suffix()}",
            "max_retries": 2,
            "sequence": 1,
            "fencing_token": 7,
            "workspace_version": "workspace-v1",
            "execution_memory": {"cursor": 1},
            "checkpoint": {"last_step": 1},
        }
        task = self.request("POST", tasks, task_body, (201,))
        node_run_id = task.get("id")
        if task.get("node_instance_id") != "noop" or task.get("attempt") != 1:
            raise AssertionError(f"NodeRun canonical fields missing: {task}")
        task_body.update({"status": "SUCCESS", "output": {"result": "ok"}})
        self.request("PUT", f"{tasks}/{node_run_id}", task_body)
        listed_tasks = self.request("GET", tasks)
        if not any(
            item.get("id") == node_run_id
            and item.get("execution_id") == task_body["execution_id"]
            and item.get("fencing_token") == 7
            for item in listed_tasks
        ):
            raise AssertionError("NodeRun is absent from canonical task list")
        if (
            self.scalar(
                connection,
                "SELECT COUNT(*) FROM orh_node_checkpoint WHERE node_run_id=%s AND fencing_token=7",
                (node_run_id,),
            )
            < 1
        ):
            raise AssertionError(
                "NodeRun checkpoint/fencing evidence was not persisted"
            )

        approvals = f"{runs}/{graph_run_id}/approvals"
        approval_id = f"approval-{self.suffix()}"
        approval_request = {
            "tool": "manual-review",
            "arguments": {"graph_run_id": graph_run_id, "node_run_id": node_run_id},
        }
        self.request(
            "POST",
            approvals,
            {
                "id": approval_id,
                "node_run_id": node_run_id,
                "type": "human_gate",
                "subject_type": "node_run",
                "subject_id": node_run_id,
                "request": approval_request,
                "idempotency_key": f"human:{graph_run_id}:{node_run_id}",
            },
            (201,),
        )
        pending = self.request("GET", approvals)
        if not any(
            item.get("id") == approval_id and item.get("status") == "pending"
            for item in pending
        ):
            raise AssertionError("Unified approval is absent from pending list")
        decision = self.request("POST", f"{approvals}/{approval_id}/approve", {})
        if decision.get("status") != "approved":
            raise AssertionError(f"Approval decision failed: {decision}")
        duplicate = self.request("POST", f"{approvals}/{approval_id}/approve", {})
        if duplicate.get("status") != "approved":
            raise AssertionError("Duplicate approval callback was not idempotent")
        if (
            self.scalar(
                connection,
                "SELECT status FROM orh_approval WHERE id=%s AND request_hash IS NOT NULL",
                (approval_id,),
            )
            != "approved"
        ):
            raise AssertionError("Unified approval terminal state was not persisted")

        self.request("PUT", f"{runs}/{graph_run_id}", {"status": "COMPLETED"})
        candidate_response = self.request(
            "POST",
            f"/api/v1/{NAMESPACE}/knowledge/candidates",
            {
                "source_graph_run_id": graph_run_id,
                "target_scope": "graph",
                "target_agent_graph_name": agent_graph_name,
                "type": "knowledge",
                "content": f"Scenario 21 approved operational fact for run {graph_run_id}.",
                "provenance": {"origin": "scenario-21", "safe": True},
                "expected_revision": 0,
                "idempotency_key": f"knowledge:{graph_run_id}",
            },
            (201,),
        )
        candidate = candidate_response.get("candidate", {})
        publication_approval = candidate_response.get("approval", {})
        candidate_id = candidate.get("id")
        publication_approval_id = publication_approval.get("id")
        if not candidate_id or not publication_approval_id:
            raise AssertionError(
                f"Candidate/approval response incomplete: {candidate_response}"
            )
        visible_before = self.request(
            "GET", f"/api/v1/{NAMESPACE}/knowledge?scope=graph"
        )
        if any(
            item.get("content") == candidate.get("content")
            for item in visible_before.get("items", [])
        ):
            raise AssertionError(
                "Unapproved Knowledge candidate became retrieval-visible"
            )
        published = self.request(
            "POST", f"{approvals}/{publication_approval_id}/approve", {}
        ).get("candidate", {})
        if published.get("status") != "published":
            raise AssertionError(f"Knowledge publication failed: {published}")
        document_id = published.get("metadata", {}).get("published_id")
        entry = self.request("GET", f"/api/v1/{NAMESPACE}/knowledge/{document_id}")
        if (
            entry.get("content") != candidate.get("content")
            or entry.get("revision") != 1
        ):
            raise AssertionError(f"Published Knowledge is incorrect: {entry}")
        self.request("POST", f"{approvals}/{publication_approval_id}/approve", {})
        if (
            self.scalar(
                connection,
                """SELECT COUNT(*) FROM knw_document d
               JOIN knw_document_revision r ON r.id=d.current_revision_id
               WHERE d.id=%s AND d.agent_graph_id=%s AND r.revision=1 AND r.status='published'""",
                (document_id, graph["id"]),
            )
            != 1
        ):
            raise AssertionError("Published Knowledge current revision is inconsistent")
        return graph_run_id

    @classmethod
    def _ignore(cls, method: str, path: str) -> None:
        try:
            cls.request(method, path, expected=(200, 204, 404), timeout=5)
        except Exception:
            pass

    def deploy(self, context: VerificationContext) -> None:
        """Use the topology prepared by the runner; no scenario reset is needed."""

    def init(self, context: VerificationContext) -> None:
        """Retain initialization ordering inside the existing scenario steps."""
