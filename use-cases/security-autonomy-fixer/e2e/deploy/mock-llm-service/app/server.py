#!/usr/bin/env python3
"""Small deterministic OpenAI-compatible server used when testing orchestration.

The fixture deliberately identifies itself as a mock.  It exercises Flowgent's
real provider client, Agent executor, schema validation, retries, tracing, and
downstream DAG without depending on third-party account credit.
"""

from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import time
import uuid


class OpenAIHandler(BaseHTTPRequestHandler):
    request_counts: dict[str, int] = {}

    def _respond(self, status: int, payload: dict[str, object]) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    @staticmethod
    def _objects(value: str) -> list[dict[str, object]]:
        decoder = json.JSONDecoder()
        objects: list[dict[str, object]] = []
        for offset, character in enumerate(value):
            if character != "{":
                continue
            try:
                candidate, _ = decoder.raw_decode(value[offset:])
            except json.JSONDecodeError:
                continue
            if isinstance(candidate, dict):
                objects.append(candidate)
        return objects

    @staticmethod
    def _plan_input(user_prompt: str) -> dict[str, object]:
        for candidate in OpenAIHandler._objects(user_prompt):
            if any(
                key in candidate
                for key in (
                    "source_files",
                    "sonarqube_issues",
                    "before_issues",
                    "original_issues",
                    "patches",
                )
            ):
                return candidate
        return {}

    @staticmethod
    def _schema(user_prompt: str) -> dict[str, object]:
        marker = "You MUST output a valid JSON object matching this schema:"
        start = user_prompt.rfind(marker)
        if start < 0:
            return {}
        for candidate in OpenAIHandler._objects(user_prompt[start + len(marker) :]):
            if candidate.get("type") == "object" and "properties" in candidate:
                return candidate
        return {}

    @staticmethod
    def _schema_value(name: str, schema: dict[str, object]) -> object:
        choices = schema.get("enum")
        if isinstance(choices, list) and choices:
            return choices[0]
        value_type = schema.get("type")
        if value_type == "object":
            properties = schema.get("properties") or {}
            required = schema.get("required") or list(properties)
            if not isinstance(properties, dict) or not isinstance(required, list):
                return {}
            return {
                key: OpenAIHandler._schema_value(key, properties.get(key) or {})
                for key in required
                if isinstance(key, str)
            }
        if value_type == "array":
            if name in {"remaining_issues", "resolved_issues"}:
                return []
            item = schema.get("items")
            return [OpenAIHandler._schema_value(name.rstrip("s"), item or {})]
        if value_type == "boolean":
            return True
        if value_type in {"integer", "number"}:
            return 1
        if name in {"path", "file"}:
            return "README.md"
        if name == "reason":
            return "Deterministic E2E policy checks passed."
        if name == "report":
            return "# Security remediation\n\nDeterministic E2E execution completed successfully."
        return f"e2e-{name.replace('_', '-')}"

    @staticmethod
    def _completion(system_prompt: str, user_prompt: str) -> tuple[str, dict[str, object]]:
        lower = user_prompt.lower()
        plan_input = OpenAIHandler._plan_input(user_prompt)

        if "parse the sonarqube scan results" in lower:
            return "aggregate", {
                "issues": [
                    {
                        "id": "e2e-sonarqube-readme",
                        "repo": "rengine",
                        "severity": "medium",
                        "type": "sast",
                        "file": "README.md",
                        "description": "Deterministic fixture finding for orchestration coverage.",
                        "source": "sonarqube",
                        "rule": "e2e:S0001",
                        "line": 1,
                        "message": "Exercise the complete remediation path.",
                    }
                ]
            }

        if "fix only the provided issue" in lower:
            source_files = plan_input.get("source_files") or []
            source = source_files[0] if isinstance(source_files, list) and source_files else {}
            path = source.get("path", "README.md") if isinstance(source, dict) else "README.md"
            content = source.get("content", "") if isinstance(source, dict) else ""
            return "fix", {
                "files": [{"path": path, "content": content}],
                "patches": [
                    {
                        "file": path,
                        "rule": "e2e:S0001",
                        "description": "Verified idempotent remediation content.",
                    }
                ],
            }

        if "compare pre-fix and post-fix" in lower:
            return "compare", {
                "resolution": "complete",
                "resolved_count": 1,
                "remaining_count": 0,
                "resolved_issues": ["e2e-sonarqube-readme"],
                "remaining_issues": [],
            }

        if "generate a final summary report" in lower:
            return "summary", {
                "report": (
                    "# Security remediation report\n\n"
                    "The isolated end-to-end workflow completed discovery, remediation, "
                    "review, delivery, rescan, and notification evidence."
                ),
                "status": "completed",
                "issues_found": 1,
                "issues_fixed": 1,
                "pr_number": 4,
            }

        if "decide the next action" in lower or "orchestration controller" in system_prompt.lower():
            return "supervisor", {
                "action": "continue",
                "target": "next",
                "reason": "All deterministic E2E safety checks passed.",
            }

        if "review each patch" in lower or "reviewer" in system_prompt.lower():
            return "review", {
                "decision": True,
                "confidence": 0.99,
                "risk_level": "low",
                "reason": "The idempotent fixture content preserves behavior and is safe.",
            }

        schema = OpenAIHandler._schema(user_prompt)
        if schema:
            return "schema", OpenAIHandler._schema_value("result", schema)
        return "default", {"status": "completed"}

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        if self.path == "/healthz":
            self._respond(
                200,
                {
                    "status": "ok",
                    "fixture": "mock-llm",
                    "request_counts": self.request_counts,
                },
            )
            return
        if self.path == "/v1/models":
            self._respond(
                200,
                {
                    "object": "list",
                    "data": [
                        {
                            "id": "e2e-deterministic",
                            "object": "model",
                            "owned_by": "flowgent-e2e",
                        }
                    ],
                },
            )
            return
        self._respond(404, {"error": {"message": "not found", "type": "not_found"}})

    def do_POST(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        if self.path != "/v1/chat/completions":
            self._respond(404, {"error": {"message": "not found", "type": "not_found"}})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(length) or b"{}")
            messages = request.get("messages") or []
            system_prompt = next(
                (item.get("content", "") for item in messages if item.get("role") == "system"),
                "",
            )
            user_prompt = next(
                (item.get("content", "") for item in messages if item.get("role") == "user"),
                "",
            )
            kind, completion = self._completion(system_prompt, user_prompt)
        except (TypeError, ValueError, json.JSONDecodeError) as error:
            self._respond(400, {"error": {"message": str(error), "type": "invalid_request"}})
            return
        self.request_counts[kind] = self.request_counts.get(kind, 0) + 1
        content = json.dumps(completion, separators=(",", ":"))
        self._respond(
            200,
            {
                "id": f"chatcmpl-e2e-{uuid.uuid4().hex}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": request.get("model", "e2e-deterministic"),
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": content},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": max(1, (len(system_prompt) + len(user_prompt)) // 4),
                    "completion_tokens": max(1, len(content) // 4),
                    "total_tokens": max(2, (len(system_prompt) + len(user_prompt) + len(content)) // 4),
                },
            },
        )

    def log_message(self, *_: object) -> None:
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), OpenAIHandler).serve_forever()
