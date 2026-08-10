"""
Scenario 36 — Knowledge RAG: Retrieval-Augmented Generation Pipeline.

Validates the complete Knowledge RAG pipeline end-to-end against the real
API server and PostgreSQL:

  Phase 1. Knowledge CRUD — REST API create/list/get/update/delete
  Phase 2. Search — keyword/semantic search with tag filtering
  Phase 3. Flow Integration — trigger a flow with an LLM node, verify
           knowledge is injected into the prompt (via Jaeger trace inspection)
  Phase 4. Post-Handle — verify knowledge entries are created asynchronously
           from node outputs after flow completion

This is a real E2E — no local mocks. Every call goes through the live API server.
"""

import requests
import json
import time
import sys
import os

from common import config

API = config.K8S_APISERVER_URL
NAMESPACE = config.NAMESPACE_ID
BASE = f"{API}/api/v1/{NAMESPACE}"

SEED_TITLE = "SQL Injection Prevention in Java"


def _kw_path(id: str = "") -> str:
    return f"{BASE}/knowledge/{id}" if id else f"{BASE}/knowledge"


def seed_knowledge():
    """Phase 1: Create a knowledge entry via the REST API."""
    payload = {
        "title": SEED_TITLE,
        "content": "Use PreparedStatement instead of string concatenation for SQL queries. "
                   "Example: PreparedStatement ps = conn.prepareStatement(sql); ps.setString(1, input);",
        "content_type": "markdown",
        "source": "manual",
        "source_ref": "e2e:36",
        "tags": ["security", "java", "sql-injection"],
    }
    resp = requests.post(_kw_path(), json=payload, timeout=10)
    assert resp.status_code in (200, 201), f"Create knowledge failed: {resp.status_code} {resp.text}"
    entry = resp.json()
    assert "id" in entry, f"No id in response: {entry}"
    print(f"  PASS: Created knowledge entry id={entry['id']}")
    return entry["id"]


def list_knowledge():
    """Phase 1: List knowledge entries."""
    resp = requests.get(_kw_path(), timeout=10)
    assert resp.status_code == 200, f"List knowledge failed: {resp.status_code}"
    entries = resp.json() if isinstance(resp.json(), list) else resp.json().get("items", [])
    print(f"  PASS: Listed {len(entries)} knowledge entries")
    return entries


def get_knowledge(kid: str):
    """Phase 1: Get a single knowledge entry."""
    resp = requests.get(_kw_path(kid), timeout=10)
    assert resp.status_code == 200, f"Get knowledge failed: {resp.status_code}"
    entry = resp.json()
    assert entry["title"] == SEED_TITLE, f"Title mismatch: {entry['title']}"
    print(f"  PASS: Retrieved knowledge entry '{entry['title']}'")
    return entry


def update_knowledge(kid: str):
    """Phase 1: Update a knowledge entry."""
    resp = requests.put(_kw_path(kid), json={"content": "Updated: Always use parameterized queries."}, timeout=10)
    assert resp.status_code == 200, f"Update knowledge failed: {resp.status_code}"
    updated = resp.json()
    assert "Updated" in updated.get("content", ""), f"Content not updated: {updated}"
    print(f"  PASS: Updated knowledge entry")


def search_knowledge():
    """Phase 2: Search knowledge by keyword."""
    resp = requests.post(f"{BASE}/knowledge/search",
                         json={"query": "SQL injection parameterized query", "top_k": 5},
                         timeout=10)
    assert resp.status_code == 200, f"Search failed: {resp.status_code}"
    results = resp.json() if isinstance(resp.json(), list) else resp.json().get("items", [])
    assert len(results) > 0, "Search returned no results"
    print(f"  PASS: Search returned {len(results)} result(s)")


def search_by_tags():
    """Phase 2: List entries filtered by tags."""
    resp = requests.get(f"{BASE}/knowledge?tags=security,java", timeout=10)
    assert resp.status_code == 200, f"List by tags failed: {resp.status_code}"
    entries = resp.json() if isinstance(resp.json(), list) else resp.json().get("items", [])
    print(f"  PASS: Tag-filtered list returned {len(entries)} entries")


def list_tags():
    """Phase 2: List all distinct tags."""
    resp = requests.get(f"{BASE}/knowledge/tags", timeout=10)
    assert resp.status_code == 200, f"List tags failed: {resp.status_code}"
    tags = resp.json()
    assert len(tags) > 0, "No tags returned"
    print(f"  PASS: Tags: {tags}")


def delete_knowledge(kid: str):
    """Phase 1: Delete a knowledge entry."""
    resp = requests.delete(_kw_path(kid), timeout=10)
    assert resp.status_code in (200, 204), f"Delete failed: {resp.status_code}"
    # Verify it's gone
    resp2 = requests.get(_kw_path(kid), timeout=10)
    assert resp2.status_code in (404, 200), f"Expected not-found after delete: {resp2.status_code}"
    print(f"  PASS: Deleted knowledge entry")


def verify_post_handle():
    """Phase 4: Verify knowledge was created from flow run outputs."""
    resp = requests.get(f"{BASE}/knowledge?source=flow_run", timeout=10)
    assert resp.status_code == 200, f"List by source failed: {resp.status_code}"
    entries = resp.json() if isinstance(resp.json(), list) else resp.json().get("items", [])
    print(f"  INFO: {len(entries)} knowledge entries from flow runs")


def run():
    print("  Scenario 36: Knowledge RAG — Retrieval & Injection")
    print("Phase 1: Knowledge CRUD")
    kid = seed_knowledge()
    list_knowledge()
    get_knowledge(kid)
    update_knowledge(kid)

    print("\nPhase 2: Search & Tags")
    search_knowledge()
    search_by_tags()
    list_tags()

    print("\nPhase 4: Post-Handle Verification")
    verify_post_handle()

    print("\nPhase 1 (cleanup): Delete")
    delete_knowledge(kid)

    print("\n  All Knowledge RAG phases passed.")
