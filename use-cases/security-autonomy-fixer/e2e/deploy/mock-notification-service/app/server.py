#!/usr/bin/env python3
"""Minimal authenticated HTTP receiver used by both notification E2E paths."""

from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hmac
import json
import os


EXPECTED_AUTHORIZATION = "Bearer " + os.environ["EXPECTED_TOKEN"]
RECEIPTS: list[dict[str, object]] = []


class ReceiverHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        authorized = hmac.compare_digest(
            self.headers.get("Authorization", ""), EXPECTED_AUTHORIZATION
        )
        if authorized:
            RECEIPTS.append({"authorized": True, "bytes": len(body)})
        self._respond(200 if authorized else 401, {"accepted": authorized})

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        if self.path == "/healthz":
            self._respond(200, {"status": "ok"})
        elif self.path == "/receipts":
            self._respond(200, {"authorized_count": len(RECEIPTS)})
        else:
            self._respond(404, {"error": "not found"})

    def _respond(self, status: int, payload: dict[str, object]) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_: object) -> None:
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), ReceiverHandler).serve_forever()
