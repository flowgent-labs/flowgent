"""Subprocess execution with deterministic output and command evidence."""

import os
import shlex
import signal
import subprocess
import sys
import tempfile
import time
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class CommandResult:
    command: tuple[str, ...]
    cwd: Path
    return_code: int
    duration_seconds: float
    output: str

    @property
    def passed(self) -> bool:
        return self.return_code == 0


class E2EProcessHelper:
    """Single command-execution boundary for deployers and verifiers."""

    @staticmethod
    def run(
        command: tuple[str, ...] | list[str],
        *,
        cwd: Path | str | None = None,
        environment: dict[str, str] | None = None,
        timeout_seconds: int = 120,
        stream: bool = True,
    ) -> CommandResult:
        """Execute one command and return the same evidence contract as AuthGuard."""
        command = tuple(str(part) for part in command)
        working_directory = Path(cwd or Path.cwd()).resolve()
        merged_environment = {**os.environ, **(environment or {})}
        started = time.monotonic()
        print(f"  $ {shlex.join(command)}", flush=True)
        try:
            with subprocess.Popen(
                command,
                env=merged_environment,
                cwd=working_directory,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                encoding="utf-8",
                errors="replace",
                **E2EProcessHelper.process_group_options(),
            ) as process:
                try:
                    stdout, stderr = process.communicate(timeout=timeout_seconds)
                except (subprocess.TimeoutExpired, KeyboardInterrupt):
                    E2EProcessHelper._terminate_tree(process)
                    raise
                completed = subprocess.CompletedProcess(
                    command, process.returncode, stdout, stderr
                )
            output = completed.stdout
            if completed.stderr:
                output += completed.stderr
            if stream and output:
                for line in output.rstrip().splitlines():
                    print(f"    {line}", flush=True)
            return_code = completed.returncode
        except subprocess.TimeoutExpired as error:
            output = "".join(
                part.decode(errors="replace") if isinstance(part, bytes) else part
                for part in (error.stdout, error.stderr)
                if part
            )
            output += f"\nTimed out after {timeout_seconds} seconds."
            return_code = 124
            if stream:
                print(f"    ERROR: timed out after {timeout_seconds}s", flush=True)
        except OSError as error:
            output = str(error)
            return_code = 127
            if stream:
                print(f"    ERROR: {error}", flush=True)
        return CommandResult(
            command=command,
            cwd=working_directory,
            return_code=return_code,
            duration_seconds=time.monotonic() - started,
            output=output.strip(),
        )

    @staticmethod
    def process_group_options() -> dict:
        if sys.platform == "win32":
            return {
                "creationflags": getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0x200)
            }
        return {"start_new_session": True}

    @staticmethod
    @contextmanager
    def private_file(content: str, *, suffix: str = ".json"):
        """Close a mode-0600 file before a child opens it, including on Windows."""
        descriptor, filename = tempfile.mkstemp(prefix="flowgent-e2e-", suffix=suffix)
        path = Path(filename)
        try:
            with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
                handle.write(content)
            yield path
        finally:
            path.unlink(missing_ok=True)

    @staticmethod
    def _terminate_tree(process: subprocess.Popen) -> None:
        """A command owns its process group, including nested make/build clients."""
        if process.poll() is not None:
            return
        if sys.platform == "win32":
            result = subprocess.run(
                ["taskkill", "/PID", str(process.pid), "/T", "/F"],
                capture_output=True,
                timeout=10,
            )
            if result.returncode and process.poll() is None:
                process.kill()
            process.communicate()
            return
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            return
        try:
            process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate()

    @classmethod
    def legacy(cls, command, timeout=120, env=None, cwd=None):
        """Temporary tuple-returning adapter for legacy component helpers."""
        result = cls.run(command, cwd=cwd, environment=env, timeout_seconds=timeout)
        return result.return_code, result.output
