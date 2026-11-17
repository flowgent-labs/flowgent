"""Local CI adapter: run the official scanner and correlate real CE analyses."""

from __future__ import annotations

import base64
import hmac
import json
import os
import re
import signal
import subprocess
import tarfile
import tempfile
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib import error, parse, request


class Scanner:
    jobs: dict[str, dict] = {}
    lock = threading.Lock()
    worker = threading.Semaphore(1)

    @staticmethod
    def api(path: str, data: dict | None = None) -> dict:
        body = None if data is None else parse.urlencode(data).encode()
        authorization = base64.b64encode(
            (os.environ["SONAR_TOKEN"] + ":").encode()
        ).decode()
        req = request.Request(
            os.environ["SONAR_HOST_URL"].rstrip("/") + path,
            data=body,
            headers={"Authorization": "Basic " + authorization},
        )
        with request.build_opener(request.ProxyHandler({})).open(
            req, timeout=30
        ) as response:
            content = response.read()
            return json.loads(content) if content else {}

    @staticmethod
    def commit(value) -> str | None:
        if isinstance(value, str):
            try:
                value = json.loads(value)
            except ValueError:
                return None
        if isinstance(value, list):
            return next((sha for item in value if (sha := Scanner.commit(item))), None)
        if not isinstance(value, dict):
            return None
        commit = value.get("commit")
        if isinstance(commit, dict) and re.fullmatch(
            r"[0-9a-f]{40}", str(commit.get("sha", ""))
        ):
            return commit["sha"]
        obj = value.get("object", {})
        if (
            isinstance(obj, dict)
            and obj.get("type") == "commit"
            and re.fullmatch(r"[0-9a-f]{40}", str(obj.get("sha", "")))
        ):
            return obj["sha"]
        if re.fullmatch(r"[0-9a-f]{40}", str(value.get("sha", ""))) and (
            "parents" in value or "tree" in value
        ):
            return value["sha"]
        for key in ("text", "content", "output", "result", "data", "structuredContent"):
            if key in value and (sha := Scanner.commit(value[key])):
                return sha
        return None

    @classmethod
    def start(cls, args: dict) -> dict:
        repo = args.get("repo", "rengine")
        ref = args.get("ref", "master")
        if repo != os.getenv("SCANNER_REPO", "rengine") or not re.fullmatch(
            r"[A-Za-z0-9][A-Za-z0-9._/-]{0,180}", ref
        ):
            raise ValueError(
                "Repository/ref is outside this fixture's configured target"
            )
        expected = None
        if args.get("require_delivery"):
            candidates = [
                cls.commit(args.get(key))
                for key in ("delivery_existing", "delivery_new")
            ]
            candidates = set(filter(None, candidates))
            if len(candidates) != 1:
                raise ValueError(
                    "Exactly one successful delivery commit is required for re-analysis"
                )
            expected = candidates.pop()
        identifier = uuid.uuid4().hex
        job = {
            "job_id": identifier,
            "status": "PENDING",
            "repo": repo,
            "ref": ref,
            "expected_commit": expected,
        }
        with cls.lock:
            cls.jobs[identifier] = job
        threading.Thread(target=cls.run, args=(identifier,), daemon=True).start()
        return dict(job)

    @classmethod
    def run(cls, identifier: str) -> None:
        job = cls.jobs[identifier]
        with cls.worker:
            job["status"] = "RUNNING"
            try:
                with tempfile.TemporaryDirectory(prefix="sonar-e2e-") as directory:
                    directory = Path(directory)
                    environment = dict(os.environ)
                    owner = os.getenv("SCANNER_OWNER", "wl4g")
                    headers = {"Accept": "application/vnd.github+json"}
                    token = environment.get("GITHUB_TOKEN") or environment.get(
                        "GH_TOKEN"
                    )
                    if token:
                        headers["Authorization"] = "Bearer " + token
                    job["phase"] = "RESOLVE_COMMIT"
                    url = f"https://api.github.com/repos/{owner}/{job['repo']}/commits/{parse.quote(job['ref'], safe='')}"
                    with request.urlopen(
                        request.Request(url, headers=headers), timeout=30
                    ) as response:
                        commit = json.load(response)["sha"]
                    if job["expected_commit"] and commit != job["expected_commit"]:
                        raise ValueError(
                            "Branch head differs from the Flow's delivered commit"
                        )
                    job["commit"] = commit
                    job["phase"] = "DOWNLOAD_SOURCE"
                    cache = (
                        Path(os.getenv("SONAR_USER_HOME", "/tmp/.sonar")) / "sources"
                    )
                    cache.mkdir(parents=True, exist_ok=True)
                    archive = cache / (commit + ".tar.gz")
                    url = f"https://codeload.github.com/{owner}/{job['repo']}/tar.gz/{commit}"
                    if not archive.is_file():
                        temporary = cache / (commit + "." + identifier + ".partial")
                        with (
                            request.urlopen(url, timeout=60) as response,
                            temporary.open("wb") as destination,
                        ):
                            count = 0
                            while block := response.read(262144):
                                count += len(block)
                                if count > 128 * 1024 * 1024:
                                    raise ValueError(
                                        "Source archive exceeds the fixture's size limit"
                                    )
                                destination.write(block)
                                job["downloaded_bytes"] = count
                        os.replace(temporary, archive)
                    job["downloaded_bytes"] = archive.stat().st_size
                    extracted = directory / "extracted"
                    with tarfile.open(archive) as source:
                        source.extractall(extracted, filter="data")
                    roots = list(extracted.iterdir())
                    if len(roots) != 1 or not roots[0].is_dir():
                        raise ValueError("GitHub source archive has an unexpected root")
                    repo_dir = roots[0]
                    job["phase"] = "ANALYSIS"
                    try:
                        cls.api(
                            "/api/components/show?component=" + parse.quote(job["repo"])
                        )
                    except error.HTTPError as failure:
                        if failure.code != 404:
                            raise
                        cls.api(
                            "/api/projects/create",
                            {
                                "project": job["repo"],
                                "name": job["repo"],
                                "mainBranch": "master",
                            },
                        )
                    properties = repo_dir / ".sonar-e2e.properties"
                    properties.write_text(
                        "\n".join(
                            (
                                "sonar.projectKey=" + job["repo"],
                                "sonar.projectName=" + job["repo"],
                                "sonar.sources=.",
                                "sonar.java.binaries=.",
                                "sonar.scm.revision=" + commit,
                                "sonar.scm.disabled=true",
                                "sonar.scanner.skipJreProvisioning=true",
                                "sonar.scanner.javaOpts=-Xms64m -Xmx512m",
                                "sonar.javascript.node.maxspace=768",
                                "sonar.nodejs.executable=/usr/bin/node",
                                "sonar.exclusions=**/.git/**,**/target/**,**/node_modules/**,**/dist/**",
                                "sonar.working.directory=" + str(directory / "work"),
                                "sonar.scanner.metadataFilePath="
                                + str(directory / "report-task.txt"),
                            )
                        )
                        + "\n"
                    )
                    cls.command(
                        [
                            "sonar-scanner",
                            "-Dproject.settings=" + str(properties),
                            "-Dsonar.host.url=" + os.environ["SONAR_HOST_URL"],
                        ],
                        environment,
                        repo_dir,
                        timeout=900,
                    )
                    report = dict(
                        line.split("=", 1)
                        for line in (directory / "report-task.txt")
                        .read_text()
                        .splitlines()
                        if "=" in line
                    )
                    task_id = report.get("ceTaskId")
                    if not task_id:
                        raise ValueError("Official scanner produced no CE task ID")
                    job["ce_task_id"] = task_id
                    job["phase"] = "CE_TASK"
                    deadline = time.monotonic() + 600
                    while time.monotonic() < deadline:
                        task = cls.api("/api/ce/task?id=" + parse.quote(task_id))[
                            "task"
                        ]
                        job["ce_status"] = task["status"]
                        if task["status"] == "SUCCESS":
                            job["analysis_id"] = task.get("analysisId")
                            break
                        if task["status"] in {"FAILED", "CANCELED"}:
                            raise ValueError(
                                "CE task failed: "
                                + task.get("errorMessage", task["status"])
                            )
                        time.sleep(2)
                    if not job.get("analysis_id"):
                        raise TimeoutError(
                            "CE task did not produce a successful analysis"
                        )
                    analyses = cls.api(
                        "/api/project_analyses/search?project="
                        + parse.quote(job["repo"])
                    )["analyses"]
                    analysis = next(
                        (row for row in analyses if row["key"] == job["analysis_id"]),
                        None,
                    )
                    if not analysis or analysis.get("revision") != commit:
                        raise ValueError(
                            "Sonar analysis revision does not match the scanned commit"
                        )
                    job["revision"] = analysis["revision"]
                    issues = cls.api(
                        "/api/issues/search?componentKeys="
                        + parse.quote(job["repo"])
                        + "&resolved=false&ps=500"
                    )
                    job["issue_ids"] = [row["key"] for row in issues.get("issues", [])]
                    job["issue_total"] = issues.get("total", 0)
                    job["status"] = "SUCCESS"
            except Exception as failure:
                if "temporary" in locals():
                    temporary.unlink(missing_ok=True)
                job["error"] = cls.redact(str(failure))
                job["status"] = "FAILED"

    @staticmethod
    def redact(text: str) -> str:
        for key in (
            "SONAR_TOKEN",
            "GITHUB_TOKEN",
            "GH_TOKEN",
            "SONAR_SCANNER_API_TOKEN",
        ):
            if os.getenv(key):
                text = text.replace(os.environ[key], "[redacted]")
        return text

    @staticmethod
    def command(args: list[str], environment: dict, cwd=None, timeout=300) -> str:
        with subprocess.Popen(
            args,
            env=environment,
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            start_new_session=True,
        ) as process:
            try:
                stdout, stderr = process.communicate(timeout=timeout)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate()
                raise TimeoutError(f"Scanner subprocess exceeded {timeout}s") from None
            if process.returncode:
                raise RuntimeError(
                    Scanner.redact(
                        f"Scanner command exited {process.returncode}:\n"
                        + (stdout + stderr)[-24000:]
                    )
                )
            return stdout

    @classmethod
    def wait(cls, value, timeout=1200) -> dict:
        while isinstance(value, dict) and "job_id" not in value:
            value = json.loads(value["text"])
        identifier = value.get("job_id") if isinstance(value, dict) else None
        if identifier not in cls.jobs:
            raise ValueError(
                "Unknown exact scanner job; latest-task fallback is forbidden"
            )
        deadline = time.monotonic() + timeout
        while cls.jobs[identifier]["status"] in {"PENDING", "RUNNING"}:
            if time.monotonic() > deadline:
                raise TimeoutError("Scanner job timed out")
            time.sleep(1)
        return dict(cls.jobs[identifier])


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def respond(self, status: int, body=None):
        payload = b"" if body is None else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def authorized(self) -> bool:
        return hmac.compare_digest(
            self.headers.get("Authorization", ""),
            "Bearer " + os.environ["SONAR_SCANNER_API_TOKEN"],
        )

    def do_GET(self):
        if self.path == "/healthz":
            return self.respond(200, {"status": "ok"})
        if not self.authorized():
            return self.respond(401, {"error": "unauthorized"})
        identifier = self.path.removeprefix("/scans/")
        return (
            self.respond(200, Scanner.jobs[identifier])
            if identifier in Scanner.jobs
            else self.respond(404)
        )

    def do_POST(self):
        if not self.authorized():
            return self.respond(401, {"error": "unauthorized"})
        message = json.loads(
            self.rfile.read(int(self.headers.get("Content-Length", 0)))
        )
        if self.path == "/scans":
            try:
                return self.respond(202, Scanner.start(message))
            except ValueError as failure:
                return self.respond(400, {"error": str(failure)})
        identifier = message.get("id")
        method = message.get("method")
        if method == "notifications/initialized":
            return self.respond(202)
        try:
            if method == "initialize":
                result = {
                    "protocolVersion": message["params"]["protocolVersion"],
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "real-sonar-scanner", "version": "1"},
                }
            elif method == "tools/list":
                result = {
                    "tools": [
                        {
                            "name": "StartAnalysis",
                            "description": "Run official SonarScanner against an exact repository commit",
                            "inputSchema": {
                                "type": "object",
                                "properties": {
                                    "repo": {"type": "string"},
                                    "ref": {"type": "string"},
                                    "require_delivery": {"type": "boolean"},
                                    "delivery_existing": {},
                                    "delivery_new": {},
                                },
                                "required": ["repo", "ref"],
                            },
                        },
                        {
                            "name": "WaitAnalysis",
                            "description": "Wait for one exact scan job and its successful CE task",
                            "inputSchema": {
                                "type": "object",
                                "properties": {"scan": {"type": "object"}},
                                "required": ["scan"],
                            },
                        },
                    ]
                }
            elif method == "tools/call":
                params = message["params"]
                if params["name"] == "StartAnalysis":
                    output = Scanner.start(params["arguments"])
                elif params["name"] == "WaitAnalysis":
                    output = Scanner.wait(params["arguments"]["scan"])
                else:
                    raise ValueError("Unknown scanner tool")
                result = {
                    "content": [{"type": "text", "text": json.dumps(output)}],
                    "isError": output.get("status") == "FAILED",
                }
            else:
                return self.respond(
                    200,
                    {
                        "jsonrpc": "2.0",
                        "id": identifier,
                        "error": {"code": -32601, "message": "Method not found"},
                    },
                )
            return self.respond(
                200, {"jsonrpc": "2.0", "id": identifier, "result": result}
            )
        except (ValueError, KeyError, TimeoutError) as failure:
            return self.respond(
                200,
                {
                    "jsonrpc": "2.0",
                    "id": identifier,
                    "error": {"code": -32602, "message": Scanner.redact(str(failure))},
                },
            )


if __name__ == "__main__":
    if not os.getenv("SONAR_SCANNER_API_TOKEN"):
        raise SystemExit("SONAR_SCANNER_API_TOKEN is required")
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
