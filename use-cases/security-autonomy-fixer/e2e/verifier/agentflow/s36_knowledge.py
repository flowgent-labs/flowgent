"""Scenario 36: approved Knowledge publication and scope-safe retrieval.

Knowledge is deliberately not a mutable CRUD resource. This verifier drives
the production safety path end to end: a completed, summary-enabled Run emits
an immutable candidate, a human approves the frozen request, publication
creates a versioned document/content hierarchy, and only then can retrieval
return the content.
"""

from __future__ import annotations

import uuid
from typing import Any

from common import config
from common.model import VerificationResult
from common.project import FlowgentE2EProject
from verifier import BaseVerifier


API = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID


class KnowledgeVerifier(BaseVerifier):
    scenario_id = "36"
    title = "Knowledge — Approved Publication and Scoped Retrieval"

    @staticmethod
    def _request(
        method: str,
        path: str,
        payload: dict[str, Any] | None = None,
        expected: tuple[int, ...] = (200,),
    ) -> Any:
        response = FlowgentE2EProject.session().request(
            method,
            f"{API}{path}",
            json=payload,
            timeout=20,
        )
        if response.status_code not in expected:
            raise AssertionError(
                f"{method} {path}: HTTP {response.status_code}, expected {expected}: "
                f"{response.text[:800]}"
            )
        if not response.text or response.status_code == 204:
            return {}
        return response.json()

    @staticmethod
    def _flow(name: str, summarize_enabled: bool) -> dict[str, Any]:
        return {
            "name": name,
            "kind": "flow",
            "description": "Scenario 36 Knowledge publication boundary",
            "summarize_enabled": summarize_enabled,
            "nodes": [{"id": "noop", "kind": "noop"}],
            "edges": [],
            "runtime_mode": "session",
        }

    @classmethod
    def _completed_run(cls, flow_name: str) -> dict[str, Any]:
        run = cls._request(
            "POST",
            f"/api/v1/{NAMESPACE}/runs",
            {
                "flow_name": flow_name,
                "status": "PENDING",
                "input": {"origin": "scenario-36"},
                "runtime_mode": "session",
            },
            (201,),
        )
        cls._request(
            "PUT",
            f"/api/v1/{NAMESPACE}/runs/{run['id']}",
            {"status": "COMPLETED"},
        )
        return cls._request("GET", f"/api/v1/{NAMESPACE}/runs/{run['id']}")

    def run(self) -> VerificationResult:
        return self.execute(
            lambda: self.step(
                "verify summary gating, unified approval, immutable publication, and scoped retrieval",
                self._verify_knowledge,
            )
        )

    def _verify_knowledge(self) -> None:
        suffix = uuid.uuid4().hex[:8]
        disabled_flow = f"knowledge-disabled-{suffix}"
        enabled_flow = f"knowledge-enabled-{suffix}"
        content = (
            f"Scenario {suffix}: prevent SQL injection by binding every untrusted value "
            "with a parameterized query instead of string concatenation."
        )

        self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/flows",
            self._flow(disabled_flow, False),
            (201,),
        )
        disabled_run = self._completed_run(disabled_flow)
        denied = FlowgentE2EProject.session().post(
            f"{API}/api/v1/{NAMESPACE}/knowledge/candidates",
            json={
                "source_run_id": disabled_run["id"],
                "target_scope": "flow",
                "target_flow_name": disabled_flow,
                "type": "knowledge",
                "content": content,
                "provenance": {"origin": "scenario-36-disabled"},
                "expected_revision": 0,
                "idempotency_key": f"scenario-36-disabled:{suffix}",
            },
            timeout=20,
        )
        if denied.status_code != 400 or "summarize_enabled=true" not in denied.text:
            raise AssertionError(
                "summary-disabled Run accepted a Knowledge candidate: "
                f"HTTP {denied.status_code} {denied.text[:400]}"
            )
        self.details.append("A completed Run with summarize_enabled=false was denied publication.")

        self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/flows",
            self._flow(enabled_flow, True),
            (201,),
        )
        run = self._completed_run(enabled_flow)
        if run.get("summarize_enabled") is not True or not run.get("context_snapshot"):
            raise AssertionError(f"Run did not freeze effective summary/snapshot state: {run}")

        candidate_response = self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/knowledge/candidates",
            {
                "source_run_id": run["id"],
                "target_scope": "flow",
                "target_flow_name": enabled_flow,
                "type": "knowledge",
                "content": content,
                "provenance": {
                    "origin": "scenario-36-reviewed-summary",
                    "source_run_id": run["id"],
                    "safe": True,
                },
                "expected_revision": 0,
                "idempotency_key": f"scenario-36:{suffix}",
            },
            (201,),
        )
        candidate = candidate_response.get("candidate", {})
        approval = candidate_response.get("approval", {})
        if candidate.get("status") != "unpublished" or approval.get("status") != "pending":
            raise AssertionError(f"Candidate was not frozen behind pending approval: {candidate_response}")
        if len(approval.get("request_hash", "")) != 64:
            raise AssertionError(f"Approval request_hash is not SHA-256: {approval}")

        before = self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/knowledge/search",
            {
                "query": suffix,
                "scope": "flow",
                "flow_name": enabled_flow,
                "top_k": 10,
            },
        )
        if any(item.get("content") == content for item in before):
            raise AssertionError("unapproved candidate leaked into retrieval")

        approval_path = (
            f"/api/v1/{NAMESPACE}/runs/{run['id']}/approvals/{approval['id']}/approve"
        )
        publication = self._request("POST", approval_path, {}).get("candidate", {})
        if publication.get("status") != "published":
            raise AssertionError(f"approved candidate was not published: {publication}")
        document_id = publication.get("metadata", {}).get("published_id")
        if not document_id:
            raise AssertionError(f"publication omitted immutable document id: {publication}")

        duplicate = self._request("POST", approval_path, {}).get("candidate", {})
        if duplicate.get("status") != "published":
            raise AssertionError(f"duplicate approval callback was not idempotent: {duplicate}")

        document = self._request(
            "GET", f"/api/v1/{NAMESPACE}/knowledge/{document_id}"
        )
        if (
            document.get("content") != content
            or document.get("revision") != 1
            or document.get("scope") != "flow"
            or document.get("flow_name") != enabled_flow
        ):
            raise AssertionError(f"published document projection is inconsistent: {document}")

        matches = self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/knowledge/search",
            {
                "query": f"{suffix} parameterized query",
                "scope": "flow",
                "flow_name": enabled_flow,
                "top_k": 10,
            },
        )
        if not any(item.get("id") == document_id and item.get("content") == content for item in matches):
            raise AssertionError(f"full-text retrieval missed approved document: {matches}")

        isolated = self._request(
            "POST",
            f"/api/v1/{NAMESPACE}/knowledge/search",
            {"query": suffix, "scope": "namespace", "top_k": 10},
        )
        if any(item.get("id") == document_id for item in isolated):
            raise AssertionError("Flow-scoped Knowledge leaked into Namespace retrieval")

        immutable_put = FlowgentE2EProject.session().put(
            f"{API}/api/v1/{NAMESPACE}/knowledge/{document_id}",
            json={"content": "tampered"},
            timeout=20,
        )
        immutable_delete = FlowgentE2EProject.session().delete(
            f"{API}/api/v1/{NAMESPACE}/knowledge/{document_id}", timeout=20
        )
        if immutable_put.status_code != 405 or immutable_delete.status_code != 405:
            raise AssertionError(
                "published Knowledge exposed mutable CRUD: "
                f"PUT={immutable_put.status_code}, DELETE={immutable_delete.status_code}"
            )

        connection = FlowgentE2EProject.pg_connect()
        if connection is None:
            raise AssertionError("PostgreSQL is required for Knowledge hierarchy evidence")
        try:
            with connection.cursor() as cursor:
                cursor.execute(
                    """SELECT c.status,a.status,length(a.request_hash),r.revision,r.status,
                              x.type,x.content,d.scope,f.name,
                              r.provenance->>'origin'
                       FROM knw_candidate c
                       JOIN orh_approval a ON a.id=c.approval_id
                       JOIN knw_document d ON d.id=%s
                       JOIN knw_document_revision r ON r.id=d.current_revision_id
                       JOIN knw_content x ON x.document_revision_id=r.id
                       LEFT JOIN orh_flow f ON f.id=d.flow_id
                       WHERE c.id=%s""",
                    (document_id, candidate["id"]),
                )
                row = cursor.fetchone()
            expected = (
                "published",
                "approved",
                64,
                1,
                "published",
                "summary",
                content,
                "flow",
                enabled_flow,
                "scenario-36-reviewed-summary",
            )
            if row != expected:
                raise AssertionError(f"Knowledge hierarchy/approval evidence mismatch: {row}")
        finally:
            connection.close()

        self.details.extend(
            [
                f"Candidate {candidate['id']} remained invisible until approval {approval['id']}.",
                f"Published document {document_id} has immutable revision 1 and summary content hierarchy.",
                "Full-text retrieval returned the approved Flow content; Namespace-scoped retrieval did not.",
                "Duplicate approval was idempotent, and PUT/DELETE are absent for published Knowledge.",
            ]
        )
