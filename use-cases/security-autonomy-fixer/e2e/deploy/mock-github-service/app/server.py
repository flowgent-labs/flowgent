"""Minimal GitHub OAuth wire contract used by the Flowgent/AuthGuard E2E."""

import json
import secrets
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlencode, urlsplit


CLIENT_ID = "e2e-flowgent-github"
CLIENT_SECRET = "e2e-flowgent-github-secret"
CODES = {}
TOKENS = {}


class Handler(BaseHTTPRequestHandler):
    def _json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urlsplit(self.path)
        query = parse_qs(parsed.query)
        if parsed.path == "/healthz":
            self._json(200, {"status": "ok", "provider": "github"})
            return
        if parsed.path == "/github/login/oauth/authorize":
            client_id = query.get("client_id", [""])[0]
            redirect_uri = query.get("redirect_uri", [""])[0]
            state = query.get("state", [""])[0]
            if client_id != CLIENT_ID or not redirect_uri or not state:
                self._json(400, {"error": "invalid authorization request"})
                return
            code = secrets.token_urlsafe(24)
            CODES[code] = {"client_id": client_id, "redirect_uri": redirect_uri}
            separator = "&" if "?" in redirect_uri else "?"
            self.send_response(302)
            self.send_header(
                "Location",
                redirect_uri + separator + urlencode({"code": code, "state": state}),
            )
            self.end_headers()
            return
        if parsed.path == "/github/user":
            token = self.headers.get("Authorization", "").removeprefix("Bearer ")
            if TOKENS.pop(token, None) != CLIENT_ID:
                self._json(401, {"error": "invalid_token"})
                return
            self._json(
                200,
                {
                    "id": 880001,
                    "login": "flowgent-security-admin",
                    "email": "flowgent-security-admin@example-corp.example",
                    "tenant_id": "example-corp",
                },
            )
            return
        self._json(404, {"error": "not_found"})

    def do_POST(self):
        if urlsplit(self.path).path != "/github/login/oauth/access_token":
            self._json(404, {"error": "not_found"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        form = parse_qs(self.rfile.read(length).decode())
        code = form.get("code", [""])[0]
        flow = CODES.pop(code, None)
        if (
            flow is None
            or form.get("client_id", [""])[0] != CLIENT_ID
            or form.get("client_secret", [""])[0] != CLIENT_SECRET
            or form.get("redirect_uri", [""])[0] != flow["redirect_uri"]
        ):
            self._json(400, {"error": "invalid_grant"})
            return
        token = secrets.token_urlsafe(32)
        TOKENS[token] = CLIENT_ID
        self._json(200, {"access_token": token, "token_type": "Bearer", "expires_in": 60})

    def log_message(self, *_):
        return


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
